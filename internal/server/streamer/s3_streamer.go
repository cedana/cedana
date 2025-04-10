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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	cedana_io "github.com/cedana/cedana/pkg/io"
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

	// FIXME - should replace w/ one-time URL
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithLogger(aws.NewConfig().Logger))
	if err != nil {
		log.Error().Err(err).Msg("failed to load AWS config")
	}

	var readFds, writeFds []*os.File
	var shardFds []string
	io := &sync.WaitGroup{}
	ioErr := make(chan error, parallelism)

	for i := range parallelism {
		// set up a profiling ctx for each parallelism
		r, w, err := os.Pipe()
		unix.FcntlInt(r.Fd(), unix.F_SETPIPE_SZ, PIPE_SIZE) // ignore if fails
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create pipe: %w", err)
		}

		readFds = append(readFds, r)
		writeFds = append(writeFds, w)
		shardFds = append(shardFds, fmt.Sprintf("%d", 3+i))
		// need a separate s3client for each parallel op
		s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			if S3Config.ForcePathStyle {
				o.UsePathStyle = true
			}
		})

		var keys []string
		if mode == READ_ONLY {
			keys, err = getKeys(ctx, s3Client, S3Config.BucketName, S3Config.KeyPrefix)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to get keys from S3: %w", err)
			}
			if len(keys) == 0 {
				return nil, nil, fmt.Errorf("no keys found in S3 bucket %s with prefix %s", S3Config.BucketName, S3Config.KeyPrefix)
			}

			// HACK, FIXME
			if int(parallelism) != len(keys) {
				return nil, nil, fmt.Errorf("number of shards (%d) does not match number of keys (%d)", len(keys), parallelism)
			}
		}

		switch mode {
		case READ_ONLY:
			// For READ_ONLY: S3 → writeFds → readFds
			io.Add(1)
			go func(pipeWriter *os.File, keys []string, shardIdx int32) {
				key := keys[shardIdx]
				defer io.Done()
				defer pipeWriter.Close()

				gid := goroutineID()
				start := time.Now()

				log.Debug().Int32("shard", shardIdx).Int64("goroutine", gid).Str("key", key).Time("start", start).
					Msg("streaming from S3: start")

				written, err := ReadFromS3(
					ctx,
					s3Client,
					pipeWriter,
					S3Config.BucketName,
					key,
				)

				end := time.Now()
				if err != nil {
					log.Error().Err(err).Int32("shard", shardIdx).Int64("goroutine", gid).Str("key", key).Dur("duration", end.Sub(start)).
						Msg("streaming from S3: failed")
					ioErr <- err
					return
				}

				log.Debug().Int32("shard", shardIdx).Int64("goroutine", gid).Str("key", key).Int64("bytesWritten", written).Dur("duration", end.Sub(start)).Msg("streaming from S3: complete")

			}(writeFds[i], keys, i)

		// For WRITE_ONLY: readFds → writeFds(streamer) → S3
		case WRITE_ONLY:
			log.Trace().Int32("shard", i).Int("write_fd", int(writeFds[i].Fd())).Msg("passing write end to CRIU")

			s3Key := fmt.Sprintf("%s/img-%d", S3Config.KeyPrefix, i)
			if compression != "none" && compression != "" {
				ext, err := cedana_io.ExtForCompression(compression)
				if err != nil {
					return nil, nil, fmt.Errorf("invalid compression format: %w", err)
				}
				s3Key += ext
			}

			io.Add(1)
			go func(pipeReader *os.File, key string, shardIdx int32) {
				defer io.Done()
				defer pipeReader.Close()

				source := &loggingReader{r: pipeReader, key: key, shard: shardIdx}

				gid := goroutineID()
				start := time.Now()

				log.Trace().Int32("shard", shardIdx).Int64("goroutine", gid).Str("key", key).Time("start", start).Msg("streaming to S3: start")

				written, err := WriteToS3(ctx, s3Client, source, S3Config.BucketName, key, compression)

				end := time.Now()
				if err != nil {
					log.Error().Err(err).Int32("shard", shardIdx).Int64("goroutine", gid).Str("key", key).Dur("duration", end.Sub(start)).Msg("streaming to S3: failed")
					ioErr <- err
					return
				}

				log.Trace().Int32("shard", shardIdx).Int64("goroutine", gid).Str("key", key).Int64("bytesWritten", written).Dur("duration", end.Sub(start)).Msg("streaming to S3: complete")

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
		if mode == WRITE_ONLY {
			log.Debug().Msg("closing all write pipes after CRIU exited")
			for _, w := range writeFds {
				_ = w.Close()
			}
		}

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
	gid := goroutineID()
	start := time.Now()

	log.Debug().
		Str("key", key).
		Int64("goroutine", gid).
		Time("start", start).
		Msg("WriteToS3: start")

	pr, pw := io.Pipe()

	compressor, err := cedana_io.NewCompressionWriter(pw, compression)
	if err != nil {
		return 0, fmt.Errorf("compression init failed: %w", err)
	}

	var (
		wg           sync.WaitGroup
		bytesWritten int64
		compressErr  error
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer pw.Close()

		compressionStart := time.Now()
		log.Trace().Str("key", key).Int64("goroutine", goroutineID()).Int64("timestamp", time.Now().Unix()).
			Msg("WriteToS3: starting compression")

		bytesWritten, compressErr = io.Copy(compressor, source)
		if cerr := compressor.Close(); cerr != nil && compressErr == nil {
			compressErr = fmt.Errorf("compressor close failed: %w", cerr)
		}

		log.Trace().Str("key", key).Int64("goroutine", goroutineID()).Int64("bytesWritten", bytesWritten).Dur("compression_duration", time.Since(compressionStart)).
			Msg("WriteToS3: compression done")
	}()

	go func() {
		<-ctx.Done()
		log.Warn().Str("key", key).Int64("goroutine", goroutineID()).
			Msg("WriteToS3: context canceled")
	}()

	uploader := manager.NewUploader(s3Client, func(u *manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024 // 5MB is S3 minimum for multipart
		u.Concurrency = 1            // Required to preserve stream ordering
		u.LeavePartsOnError = true   // For diagnosing failures
	})

	uploadStart := time.Now()
	log.Debug().Str("key", key).Int64("goroutine", gid).Msg("WriteToS3: starting S3 upload")

	_, uploadErr := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   pr,
	})

	log.Trace().Str("key", key).Int64("goroutine", gid).Dur("upload_duration", time.Since(uploadStart)).
		Msg("WriteToS3: upload finished")

	wg.Wait()
	end := time.Now()

	log.Debug().Str("key", key).Int64("goroutine", gid).Dur("total_duration", end.Sub(start)).
		Msg("WriteToS3: complete")

	if compressErr != nil {
		return 0, fmt.Errorf("compression failed: %w", compressErr)
	}
	if uploadErr != nil {
		return 0, fmt.Errorf("upload failed: %w", uploadErr)
	}

	return bytesWritten, nil
}

func ReadFromS3(ctx context.Context, s3Client *s3.Client, dest io.Writer, bucket, key string) (int64, error) {
	gid := goroutineID()
	start := time.Now()
	log.Trace().Int64("goroutine", gid).Str("key", key).Time("start", start).Msg("ReadFromS3: start")

	getResp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("GetObject failed: %w", err)
	}
	defer getResp.Body.Close()

	ext := filepath.Ext(key)
	compression := strings.TrimPrefix(ext, ".")
	log.Trace().Int64("goroutine", gid).Str("key", key).Str("compression", compression).
		Msg("ReadFromS3: initializing decompression")

	reader, err := cedana_io.NewCompressionReader(getResp.Body, compression)
	if err != nil {
		return 0, fmt.Errorf("decompression init failed: %w", err)
	}

	pr, pw := io.Pipe()

	var decompressErr error
	go func() {
		defer pw.Close()
		defer reader.Close()

		_, decompressErr = io.Copy(pw, reader)
		if decompressErr != nil {
			log.Error().Int64("goroutine", gid).Str("key", key).Err(decompressErr).
				Msg("ReadFromS3: decompression failed")
		}
	}()

	// Stream decompressed data into dest
	written, err := io.Copy(dest, pr)
	end := time.Now()

	if err != nil {
		return written, fmt.Errorf("pipe → dest copy failed: %w", err)
	}
	if decompressErr != nil {
		return written, fmt.Errorf("decompression failed: %w", decompressErr)
	}

	log.Debug().Int64("goroutine", gid).Str("key", key).Int64("bytesRead", written).Dur("duration", end.Sub(start)).
		Msg("ReadFromS3: complete")
	return written, nil
}

func goroutineID() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := bytes.Fields(buf[:n])[1]
	id, _ := strconv.ParseInt(string(idField), 10, 64)
	return id
}

type loggingReader struct {
	r     io.Reader
	key   string
	shard int32
}

func (lr *loggingReader) Read(p []byte) (int, error) {
	log.Debug().
		Str("key", lr.key).
		Int32("shard", lr.shard).
		Msg("waiting to read from pipe...")

	n, err := lr.r.Read(p)

	log.Debug().
		Str("key", lr.key).
		Int32("shard", lr.shard).
		Int("n", n).
		Msg("read from pipe")

	return n, err
}

// Gets keys for a jobID. Required for restore side of eq
func getKeys(ctx context.Context, s3Client *s3.Client, bucket, keyPrefix string) ([]string, error) {
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(keyPrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list keys for prefix '%s' failed: %w", keyPrefix, err)
		}
		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
	}

	return keys, nil
}
