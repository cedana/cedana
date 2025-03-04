package streamer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

// S3Config holds configuration for S3 storage
type S3Config struct {
	Endpoint       string
	Region         string
	BucketName     string
	KeyPrefix      string
	AccessKey      string
	SecretKey      string
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
	s3Config S3Config,
	compression string,
) (*Fs, func() error, error) {
	var opts []func(*config.LoadOptions) error
	opts = append(opts, config.WithRegion(s3Config.Region))

	if s3Config.AccessKey != "" && s3Config.SecretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(s3Config.AccessKey, s3Config.SecretKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	var readFds, writeFds []*os.File
	var shardFds []string
	io := &sync.WaitGroup{}
	ioErr := make(chan error, parallelism)

	for i := range parallelism {
		r, w, err := os.Pipe()
		unix.FcntlInt(r.Fd(), unix.F_SETPIPE_SZ, PIPE_SIZE) // ignore if fails
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create pipe: %w", err)
		}

		readFds = append(readFds, r)
		writeFds = append(writeFds, w)
		shardFds = append(shardFds, fmt.Sprintf("%d", 3+i))

		s3Key := fmt.Sprintf("%s/img-%d", s3Config.KeyPrefix, i)
		if compression != "none" && compression != "" {
			ext, err := cedana_io.ExtForCompression(compression)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid compression format: %w", err)
			}
			s3Key += ext
		}

		// need a separate s3client for each parallel op
		s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			if s3Config.Endpoint != "" {
				o.BaseEndpoint = aws.String(s3Config.Endpoint)
			}
			if s3Config.ForcePathStyle {
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

				_, err := ReadFromS3(ctx, s3Client, s3Config.BucketName, key, pipeWriter, compression)
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
			go func(pipeReader *os.File, key string, shardIdx int) {
				defer io.Done()
				defer pipeReader.Close()

				log.Debug().Int("shard", shardIdx).Str("key", key).Msg("streaming to S3")

				// Create a temporary file to hold the data for S3
				tmpFile, err := os.CreateTemp(tempDir, fmt.Sprintf("s3-upload-%d-*", shardIdx))
				if err != nil {
					log.Error().Err(err).Msg("failed to create temp file")
					ioErr <- err
					return
				}
				tmpPath := tmpFile.Name()
				tmpFile.Close() // Close it first, WriteTo will open it
				defer os.Remove(tmpPath)

				// Use your utility function to write from pipe to file with compression
				_, err = utils.WriteTo(pipeReader, tmpPath, compression)
				if err != nil {
					log.Error().Err(err).Str("key", key).Msg("failed to prepare file for S3")
					ioErr <- err
					return
				}

				// Now upload the file to S3
				file, err := os.Open(tmpPath)
				if err != nil {
					log.Error().Err(err).Str("path", tmpPath).Msg("failed to open temp file")
					ioErr <- err
					return
				}
				defer file.Close()

				_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
					Bucket: aws.String(s3Config.BucketName),
					Key:    aws.String(key),
					Body:   file,
				})

				if err != nil {
					log.Error().Err(err).Str("key", key).Msg("failed to upload to S3")
					ioErr <- err
					return
				}

				log.Debug().Int("shard", shardIdx).Str("key", key).Msg("finished streaming to S3")
			}(readFds[i], s3Key, i)
		}
	}

	// Start the streamer binary
	args := []string{"--images-dir", tempDir}
	var extraFiles []*os.File

	switch mode {
	case READ_ONLY:
		args = append(args, "--shard-fds", strings.Join(shardFds, ","), "serve")
		extraFiles = readFds
	case WRITE_ONLY:
		args = append(args, "--shard-fds", strings.Join(shardFds, ","), "capture")
		extraFiles = writeFds
	}

	cmd := exec.CommandContext(ctx, streamerBinary, args...)
	cmd.ExtraFiles = extraFiles
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Set up socket communication
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

	// Wait for the streamer to be ready
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

	// Create wait function
	wait := func() error {
		// Stop the listener and close connection
		fs.stopListener()
		fs.conn.Close()

		// Wait for all IO to finish
		ioWg.Wait()
		close(ioErr)

		// Return any errors
		for err := range ioErr {
			if err != nil {
				return err
			}
		}

		return nil
	}

	return fs, wait, nil
}

func ReadFromS3(
	ctx context.Context,
	s3Client *s3.Client,
	bucket, key string,
	target *os.File,
	compression string) (int64, error) {
	defer target.Close()

	resp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get object from S3: %w", err)
	}
	defer resp.Body.Close()

	// Decompress the stream based on compression format
	reader, err := cedana_io.NewCompressionReader(resp.Body, compression)
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	isPipe, err := cedana_io.IsPipe(target.Fd())
	if err != nil {
		return 0, err
	}

	if compression == "none" && isPipe {
		return io.Copy(target, reader)
	} else {
		return io.Copy(target, reader)
	}
}

func WriteToS3(
	ctx context.Context,
	s3Client *s3.Client,
	source *os.File,
	bucket, key, compression string) (int64, error) {
	defer source.Close()

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		writer, err := cedana_io.NewCompressionWriter(pw, compression)
		if err != nil {
			log.Error().Err(err).Msg("failed to create compression writer")
			pw.CloseWithError(err)
			return
		}
		defer writer.Close()

		written, err := io.Copy(writer, source)
		if err != nil {
			pw.CloseWithError(err)
			log.Error().Err(err).Msg("failed to compress data")
			return
		}

		log.Debug().Int64("bytes", written).Msg("compressed data")
	}()

	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   pr,
	})

	if err != nil {
		return 0, fmt.Errorf("failed to upload to S3: %w", err)
	}

	return 0, nil
}
