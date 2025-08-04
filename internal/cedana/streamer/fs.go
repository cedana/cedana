package streamer

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana-image-streamer/protocolbuffers/go/img_streamer"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

type Mode int

// Image streamer can only be in one of these modes, never both
const (
	READ_ONLY Mode = iota
	WRITE_ONLY
)

const (
	CAPTURE_SOCK       = "streamer-capture.sock"
	SERVE_SOCK         = "streamer-serve.sock"
	INIT_PROGRESS_MSG  = "socket-init"
	STOP_LISTENER_MSG  = "stop-listener"
	IMG_FILE_PATTERN   = "^img-*"
	IMG_FILE_FORMATTER = "img-%d"
	CONNECTION_TIMEOUT = 1 * time.Minute
	PIPE_SIZE          = 4 * utils.MEBIBYTE
)

// Implementation of the afero.Fs filesystem interface that uses streaming as the backend
// using the streamer plugin.
type Fs struct {
	mode Mode
	conn *net.UnixConn
	dir  string
}

// For READ_ONLY mode, compression is automatically determined.
// For WRITE_ONLY mode, compression may be specified.
// Returns a wait function that *must* be called to tell the streamer to shutdown,
// and wait for it to finish streaming and exit gracefully. The wait function returns
// any IO errors that occurred during the streaming process.
func NewStreamingFs(
	ctx context.Context,
	streamerBinary string,
	imagesDir string,
	storage cedana_io.Storage,
	storagePath string,
	streams int32,
	mode Mode,
	compressions ...string,
) (fs *Fs, wait func() error, err error) {
	if streams < 1 {
		return nil, nil, fmt.Errorf("invalid number of streams: %d", streams)
	}
	var compression string
	if len(compressions) > 0 {
		compression = compressions[0]
	}

	// Create pipes for reading and writing data to/from the streamer to dir
	var readFds, writeFds []*os.File
	var shardFds []string
	for i := range streams {
		r, w, err := os.Pipe()
		unix.FcntlInt(r.Fd(), unix.F_SETPIPE_SZ, PIPE_SIZE) // ignore if fails
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create pipe: %w", err)
		}
		readFds = append(readFds, r)
		writeFds = append(writeFds, w)
		shardFds = append(shardFds, fmt.Sprintf("%d", 3+i))
	}

	// Start IO on the pipes from the dir
	wg := &sync.WaitGroup{}
	io := &sync.WaitGroup{}
	ioErr := make(chan error, streams)
	paths, err := imgPaths(storage, storagePath, mode, streams)
	if err != nil {
		return nil, nil, err
	}
	for i := range streams {
		switch mode {
		case READ_ONLY:
			defer readFds[i].Close()
			compression, err := cedana_io.CompressionFromExt(paths[i])
			if err != nil {
				return nil, nil, err
			}
			file, err := storage.Open(paths[i])
			if err != nil {
				return nil, nil, err
			}
			io.Add(1)
			go func() {
				defer io.Done()
				defer func() {
					ioErr <- file.Close()
				}()

				_, err = cedana_io.ReadFrom(file, writeFds[i], compression)
				writeFds[i].Close()
				if err != nil {
					ioErr <- err
				}
			}()
		case WRITE_ONLY:
			defer writeFds[i].Close()
			ext, err := cedana_io.ExtForCompression(compression)
			if err != nil {
				return nil, nil, err
			}
			path := paths[i] + ext
			file, err := storage.Create(path)
			if err != nil {
				return nil, nil, err
			}
			io.Add(1)
			go func() {
				defer io.Done()
				defer func() {
					ioErr <- file.Close()
				}()

				_, err = cedana_io.WriteTo(readFds[i], file, compression)
				readFds[i].Close()
				if err != nil {
					ioErr <- err
				}
			}()
		}
	}

	args := []string{"--images-dir", imagesDir}
	var extraFiles []*os.File
	var lastMsg string

	switch mode {
	case READ_ONLY:
		args = append(args, "--shard-fds", strings.Join(shardFds, ","), "serve")
		extraFiles = readFds
	case WRITE_ONLY:
		args = append(args, "--shard-fds", strings.Join(shardFds, ","), "capture")
		extraFiles = writeFds
	default:
		return nil, nil, fmt.Errorf("invalid mode: %d", mode)
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
			lastMsg = scanner.Text()
			if lastMsg == INIT_PROGRESS_MSG {
				ready <- true
			}
			log.Trace().Str("context", "streamer").Str("dir", imagesDir).Msg(lastMsg)
		}
	}()

	err = cmd.Start()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start streamer: %w", err)
	}

	fs = &Fs{mode, nil, imagesDir}

	// Clean up on exit
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(exited)

		err := cmd.Wait()
		if err != nil {
			log.Trace().Err(err).Msg("streamer Wait()")
		}
		log.Debug().Str("dir", imagesDir).Int("code", cmd.ProcessState.ExitCode()).Msg("streamer exited")

		// FIXME: Remove socket files. Should be cleaned up by the streamer itself
		matches, err := filepath.Glob(filepath.Join(imagesDir, "*.sock"))
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
		return nil, nil, fmt.Errorf("timed out waiting for streamer to start: %s", lastMsg)
	case <-ready:
	case <-exited:
	}

	// Connect to the streamer
	var conn net.Conn
	switch mode {
	case READ_ONLY:
		conn, err = net.Dial("unix", filepath.Join(imagesDir, SERVE_SOCK))
	case WRITE_ONLY:
		conn, err = net.Dial("unix", filepath.Join(imagesDir, CAPTURE_SOCK))
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to streamer: %w: %s", err, lastMsg)
	}
	fs.conn = conn.(*net.UnixConn)
	log.Debug().Str("dir", imagesDir).Msg("streamer connected")

	wait = func() error {
		// Stop the listener, and wait for all IO to finish
		// NOTE: The order of below operations is important.
		fs.stopListener()
		fs.conn.Close()
		io.Wait()
		cmd.Process.Signal(syscall.SIGTERM)
		wg.Wait()
		close(ioErr)
		var err error
		for e := range ioErr {
			err = errors.Join(err, e)
		}
		return err
	}

	return fs, wait, nil
}

func (fs *Fs) Create(name string) (afero.File, error) {
	if fs.mode != WRITE_ONLY {
		return nil, fmt.Errorf("create failed: streaming filesystem not open for writing")
	}
	fd, err := fs.openFd(name)
	if err != nil {
		return nil, err
	}
	return &File{name, fs.mode, fd}, nil
}

func (fs *Fs) Open(name string) (afero.File, error) {
	fd, err := fs.openFd(name)
	if err != nil {
		return nil, err
	}
	return &File{name, fs.mode, fd}, nil
}

func (fs *Fs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	return nil, fmt.Errorf("not implemented for streaming")
}

func (fs *Fs) Remove(name string) error {
	return fmt.Errorf("not implemented for streaming")
}

func (fs *Fs) RemoveAll(path string) error {
	return fmt.Errorf("not implemented for streaming")
}

func (fs *Fs) Rename(oldname, newname string) error {
	return fmt.Errorf("not implemented for streaming")
}

func (fs *Fs) Mkdir(name string, perm os.FileMode) error {
	return fmt.Errorf("not implemented for streaming")
}

func (fs *Fs) MkdirAll(path string, perm os.FileMode) error {
	return fmt.Errorf("not implemented for streaming")
}

func (fs *Fs) Stat(name string) (os.FileInfo, error) {
	return nil, fmt.Errorf("not implemented for streaming")
}

func (fs *Fs) Chown(name string, uid, gid int) error {
	return fmt.Errorf("not implemented for streaming")
}

func (fs *Fs) Chmod(name string, mode os.FileMode) error {
	return fmt.Errorf("not implemented for streaming")
}

func (fs *Fs) Chtimes(name string, atime, mtime time.Time) error {
	return fmt.Errorf("not implemented for streaming")
}

func (fs *Fs) Name() string {
	return fs.dir
}

////////////////////
// Helper Methods //
////////////////////

// Opens a pair of file descriptors for reading and writing through the streamer
func (fs *Fs) openFd(name string) (int, error) {
	fds := make([]int, 2)
	err := syscall.Pipe(fds)
	if err != nil {
		return 0, err
	}
	rFd, wFd := fds[0], fds[1]

	var streamerFd, ourFd int

	switch fs.mode {
	case READ_ONLY:
		streamerFd = wFd // streamer should be able to write to this fd
		ourFd = rFd      // we should be able to read from this fd
	case WRITE_ONLY:
		streamerFd = rFd // streamer should be able to read from this fd
		ourFd = wFd      // we should be able to write to this fd
	default:
		return 0, fmt.Errorf("invalid mode: %d", fs.mode)
	}
	defer syscall.Close(streamerFd) // close it after sending it to streamer
	defer func() {
		if err != nil {
			syscall.Close(ourFd)
		}
	}()

	// Send file request to streamer
	req := &img_streamer.ImgStreamerRequestEntry{Filename: name}
	data, err := proto.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal request: %w", err)
	}
	size := len(data)
	var sizeBuf [4]byte
	binary.LittleEndian.PutUint32(sizeBuf[:], uint32(size))
	_, err = fs.conn.Write(sizeBuf[:])
	if err != nil {
		return 0, fmt.Errorf("failed to write size to file request: %w", err)
	}
	_, err = fs.conn.Write(data)
	if err != nil {
		return 0, fmt.Errorf("failed to write data to file request: %w", err)
	}

	// If read-only, read for msg from streamer if file exists
	if fs.mode == READ_ONLY {
		resp := &img_streamer.ImgStreamerReplyEntry{}
		var sizeBuf [4]byte
		_, err = fs.conn.Read(sizeBuf[:])
		if err != nil {
			return 0, fmt.Errorf("failed to read size from file response: %w", err)
		}
		size := binary.LittleEndian.Uint32(sizeBuf[:])
		data := make([]byte, size)
		n, err := fs.conn.Read(data)
		if err != nil {
			return 0, fmt.Errorf("failed to read data from file response: %w", err)
		}
		if n != int(size) {
			return 0, fmt.Errorf("failed to read data from file response: expected %d bytes, got %d", size, n)
		}
		err = proto.Unmarshal(data, resp)
		if err != nil {
			return 0, fmt.Errorf("failed to unmarshal response: %w", err)
		}
		if !resp.Exists {
			return 0, fmt.Errorf("file does not exist: %s", name)
		}
	}

	sock, err := fs.conn.File()
	if err != nil {
		return 0, fmt.Errorf("failed to get file from connection: %w", err)
	}
	defer sock.Close()
	rights := syscall.UnixRights(streamerFd)
	err = syscall.Sendmsg(int(sock.Fd()), nil, rights, nil, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to send streamer fd: %w", err)
	}

	return ourFd, nil
}

// Tells the streamer to stop listening for new connections
func (fs *Fs) stopListener() error {
	// Send file request to streamer
	req := &img_streamer.ImgStreamerRequestEntry{Filename: STOP_LISTENER_MSG}
	data, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}
	size := len(data)
	buf := make([]byte, 4+size)
	binary.LittleEndian.PutUint32(buf, uint32(size))
	copy(buf[4:], data)
	_, err = fs.conn.Write(buf)
	if err != nil {
		return fmt.Errorf("failed to write data to file request: %w", err)
	}

	return nil
}

// Returns a list of image paths found in the image directory.
// Returns an error if the number of images found is not equal to the number of streams specified.
func imgPaths(storage cedana_io.Storage, dir string, mode Mode, streams int32) ([]string, error) {
	switch mode {
	case READ_ONLY:
		list, err := storage.ReadDir(dir)
		if err != nil {
			return nil, err
		}

		matches := make([]string, 0, len(list))
		for _, entry := range list {
			entry := filepath.Base(entry)
			if regexp.MustCompile(IMG_FILE_PATTERN).MatchString(entry) {
				matches = append(matches, dir+"/"+entry)
			}
		}

		if len(matches) != int(streams) {
			return nil, fmt.Errorf("expected %d images, got %d. please specify correct number of streams", streams, len(matches))
		}

		return matches, nil
	case WRITE_ONLY:
		paths := make([]string, streams)
		for i := range streams {
			paths[i] = dir + "/" + fmt.Sprintf(IMG_FILE_FORMATTER, i)
		}
		return paths, nil
	default:
		return nil, fmt.Errorf("invalid mode: %d", mode)
	}
}

func IsStreamable(storage cedana_io.Storage, dir string) (streams int32, err error) {
	if storage == nil {
		return 0, fmt.Errorf("storage is nil")
	}

	isDir, err := storage.IsDir(dir)
	if err != nil {
		return 0, fmt.Errorf("failed to check if path is a directory: %w", err)
	}

	if !isDir {
		return 0, nil
	}

	list, err := storage.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("failed to read dir: %w", err)
	}

	matches := 0
	for _, entry := range list {
		entry := filepath.Base(entry)
		if regexp.MustCompile(IMG_FILE_PATTERN).MatchString(entry) {
			matches++
		}
	}

	return int32(matches), nil
}
