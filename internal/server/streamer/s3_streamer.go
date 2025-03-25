package streamer

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

// S3Config holds configuration for S3 storage
type S3Config struct {
	BucketName     string
	KeyPrefix      string
	ForcePathStyle bool
}

// NewS3StreamingFs creates a streaming filesystem that connects directly to S3
func NewS3StreamingFs(
	ctx context.Context,
	wg *sync.WaitGroup,
	streamerBinary string,
	tempDir string,
	parallelism int32,
	mode Mode,
	compression string,
	S3Config S3Config,
) (*Fs, func() error, error) {

	// Load AWS config
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to load AWS config")
	}

	var readFds, writeFds []*os.File
	var shardFds []string
	io := &sync.WaitGroup{}
	ioErr := make(chan error, parallelism)

	for i := range parallelism {
		// set up a profiling ctx for each parallelism
		s3ctx, end := profiling.StartTimingComponent(ctx, "streamerWrite")
		r, w, err := os.Pipe()
		unix.FcntlInt(r.Fd(), unix.F_SETPIPE_SZ, PIPE_SIZE) // ignore if fails
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create pipe: %w", err)
		}

		readFds = append(readFds, r)
		writeFds = append(writeFds, w)
		shardFds = append(shardFds, fmt.Sprintf("%d", 3+i))

		s3Key := fmt.Sprintf("%s/img-%d", S3Config.KeyPrefix, i)
		if compression != "none" && compression != "" {
			ext, err := cedana_io.ExtForCompression(compression)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid compression format: %w", err)
			}
			s3Key += ext
		}

		// need a separate s3client for each parallel op
		s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			if S3Config.ForcePathStyle {
				o.UsePathStyle = true
			}
		})

		switch mode {
		case READ_ONLY:
			// For READ_ONLY: S3 → writeFds → readFds(your code)
			io.Add(1)
			go func(pipeWriter *os.File, key string, shardIdx int32) {
				defer io.Done()
				defer pipeWriter.Close()

				log.Debug().Int32("shard", shardIdx).Str("key", key).Msg("streaming from S3")

				_, err := ReadFromS3(
					ctx,
					s3Client,
					pipeWriter,
					S3Config.BucketName,
					key)

				if err != nil {
					log.Error().Err(err).Str("key", key).Msg("failed to stream from S3")
					ioErr <- err
					return
				}
				log.Debug().Int32("shard", shardIdx).Str("key", key).Msg("finished streaming from S3")
			}(writeFds[i], s3Key, i)

		case WRITE_ONLY:
			// For WRITE_ONLY: readFds → writeFds(streamer) → S3
			io.Add(1)
			go func(pipeReader *os.File, key string, shardIdx int32) {
				defer io.Done()
				defer pipeReader.Close()

				log.Debug().Int32("shard", shardIdx).Str("key", key).Msg("streaming to S3")

				_, err := WriteToS3(s3ctx, s3Client, pipeReader, S3Config.BucketName, key, compression)
				if err != nil {
					log.Error().Err(err).Str("key", key).Msg("failed to upload to S3")
					ioErr <- err
					return
				}

				log.Debug().Int32("shard", shardIdx).Str("key", key).Msg("finished streaming to S3")
				end()
			}(readFds[i], s3Key, i)
		}
	}

	args := []string{"--images-dir", tempDir}
	var extraFiles []*os.File

	switch mode {
	case READ_ONLY:
		args = append(args, "--shard-fds", strings.Join(shardFds, ","), "serve")
		extraFiles = readFds
	case WRITE_ONLY:
		args = append(args, "--shard-fds", strings.Join(shardFds, ","), "capture")
		extraFiles = writeFds
	default:
		return nil, nil, fmt.Errorf("invalid mode: %v", mode)
	}

	cmd := exec.CommandContext(ctx, streamerBinary, args...)
	cmd.ExtraFiles = extraFiles
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) } // AVOID SIGKILL
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	ready := make(chan bool, 1)
	exited := make(chan bool, 1)
	defer close(ready)

	// Mark ready when we read init progress message on stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		for {
			if !scanner.Scan() || ctx.Err() != nil {
				break
			}
			if scanner.Text() == INIT_PROGRESS_MSG {
				ready <- true
			}
			log.Trace().Str("context", "streamer").Str("dir", tempDir).Msg(scanner.Text())
		}
	}()

	err = cmd.Start()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start streamer: %w", err)
	}

	fs := &Fs{mode, nil, tempDir}

	// Clean up on exit
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(exited)

		err := cmd.Wait()
		if err != nil {
			log.Trace().Err(err).Msg("streamer Wait()")
		}
		log.Trace().Int("code", cmd.ProcessState.ExitCode()).Msg("streamer exited")

		// Clean up socket files
		matches, err := filepath.Glob(filepath.Join(tempDir, "*.sock"))
		if err == nil {
			for _, match := range matches {
				os.Remove(match)
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-time.After(CONNECTION_TIMEOUT):
		return nil, nil, fmt.Errorf("timed out waiting for streamer to start")
	case <-ready:
	case <-exited:
	}

	// Connect to the streamer
	var conn net.Conn
	switch mode {
	case READ_ONLY:
		conn, err = net.Dial("unix", filepath.Join(tempDir, SERVE_SOCK))
	case WRITE_ONLY:
		conn, err = net.Dial("unix", filepath.Join(tempDir, CAPTURE_SOCK))
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to streamer: %w", err)
	}
	fs.conn = conn.(*net.UnixConn)
	log.Debug().Msg("streamer connected")

	signal.Ignored(syscall.SIGPIPE) // Avoid program termination due to broken pipe

	wait := func() error {
		fs.stopListener()
		fs.conn.Close()
		io.Wait()
		signal.Reset(syscall.SIGPIPE)
		close(ioErr)
		select {
		case err := <-ioErr:
			return err
		default:
			return nil
		}
	}
	return fs, wait, nil
}

func WriteToS3(
	ctx context.Context,
	s3Client *s3.Client,
	source io.Reader,
	bucket, key, compression string,
) (int64, error) {
	pr, pw := io.Pipe()
	defer pr.Close()

	compressErr := make(chan error, 1)
	var bytesWritten int64

	// Goroutine 1: Compress data into the pipe (streaming)
	go func() {
		defer pw.Close()
		compressor, err := cedana_io.NewCompressionWriter(pw, compression)
		if err != nil {
			compressErr <- fmt.Errorf("compression init failed: %w", err)
			return
		}
		defer compressor.Close()

		// Stream source → compressor → pipe
		written, err := io.Copy(compressor, source)
		bytesWritten = written
		if err != nil {
			compressErr <- fmt.Errorf("compression failed: %w", err)
			return
		}
		compressErr <- nil
	}()

	// Goroutine 2: Upload from a seekable buffer
	uploadDone := make(chan error, 1)
	go func() {
		// Wrap the pipe in a buffered ReadSeeker (simulates seeking)
		seeker := newBufferedReadSeeker(pr, 5*1024*1024) // 5MB buffer
		_, err := manager.NewUploader(s3Client).Upload(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
			Body:   seeker,
		})
		uploadDone <- err
	}()

	select {
	case err := <-compressErr:
		if err != nil {
			return 0, fmt.Errorf("compression error: %w", err)
		}
	case err := <-uploadDone:
		if err != nil {
			return 0, fmt.Errorf("upload error: %w", err)
		}
	}

	return bytesWritten, nil
}

type bufferedReadSeeker struct {
	r      io.Reader
	buf    *bytes.Buffer
	offset int64
}

func newBufferedReadSeeker(r io.Reader, bufSize int) io.ReadSeeker {
	return &bufferedReadSeeker{
		r:   r,
		buf: bytes.NewBuffer(make([]byte, 0, bufSize)),
	}
}

func (b *bufferedReadSeeker) Read(p []byte) (n int, err error) {
	n, err = b.r.Read(p)
	if n > 0 {
		b.buf.Write(p[:n]) // Keep a rolling buffer
		b.offset += int64(n)
	}
	return
}

func (b *bufferedReadSeeker) Seek(offset int64, whence int) (int64, error) {
	// Allow S3 to "seek" within the buffered window
	if whence == io.SeekStart && offset >= b.offset-int64(b.buf.Len()) {
		return offset, nil // Pretend it worked
	}
	return b.offset, nil
}

func ReadFromS3(
	ctx context.Context,
	s3Client *s3.Client,
	dest io.Writer,
	bucket, key string,
) (int64, error) {
	// Download the object from S3
	resp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to download object: %w", err)
	}
	defer resp.Body.Close()

	compression, err := cedana_io.CompressionFromExt(key)
	if err != nil {
		return 0, fmt.Errorf("failed to infer compression type: %w", err)
	}

	var reader io.Reader = resp.Body
	if compression != "" {
		decompressor, err := cedana_io.NewCompressionReader(resp.Body, compression)
		if err != nil {
			return 0, fmt.Errorf("failed to create decompression reader: %w", err)
		}
		defer decompressor.Close()
		reader = decompressor
	}

	// Copy the data to the destination writer
	written, err := io.Copy(dest, reader)
	if err != nil {
		return 0, fmt.Errorf("failed to write data to destination: %w", err)
	}

	return written, nil
}
