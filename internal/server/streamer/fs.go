package streamer

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana-image-streamer/protocolbuffers/go/img_streamer"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"google.golang.org/protobuf/proto"
)

type Mode int

const (
	READ_ONLY Mode = iota
	WRITE_ONLY
)

const (
	CAPTURE_SOCK        = "streamer-capture.sock"
	SERVE_SOCK          = "streamer-serve.sock"
	INIT_PROGRESS_MSG   = "socket-init"
	STOP_LISTENER_MSG   = "stop-listener"
	IMG_FILE_PATTERN    = "img-*"
	IMG_FILE_FORMATTER  = "img-%d"
	CONNECTION_TIMEOUT  = 5 * time.Second
	DEFAULT_PARALLELISM = 4
)

// Implementation of the afero.Fs filesystem interface that uses streaming as the backend
// using the streamer plugin.
type Fs struct {
	mode Mode
	conn *net.UnixConn
	afero.Fs
}

// For READ_ONLY mode, compression is automatically determined.
// For WRITE_ONLY mode, compression may be specified.
// Returns a wait function that *must* be called to tell the streamer to shutdown,
// and wait for it to finish streaming and exit gracefully. This function returns
// any IO errors that occurred during the streaming process.
func NewStreamingFs(
	ctx context.Context,
	wg *sync.WaitGroup,
	streamerBinary string,
	dir string,
	parallelism int32,
	mode Mode,
	compressions ...string,
) (fs *Fs, wait func() error, err error) {
	if parallelism < 1 {
		parallelism = DEFAULT_PARALLELISM
	}
	var compression string
	if len(compressions) > 0 {
		compression = compressions[0]
	}

	// Create pipes for reading and writing data to/from the streamer to dir
	var readFds, writeFds []*os.File
	var shardFds []string
	for i := range parallelism {
		r, w, err := os.Pipe()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create pipe: %w", err)
		}
		readFds = append(readFds, r)
		writeFds = append(writeFds, w)
		shardFds = append(shardFds, fmt.Sprintf("%d", 3+i))
	}

	// Start IO on the pipes from the dir
	io := &sync.WaitGroup{}
	ioErr := make(chan error, 1)
	paths, err := imgPaths(dir, mode, parallelism)
	if err != nil {
		return nil, nil, err
	}
	for i := range parallelism {
		io.Add(1)
		switch mode {
		case READ_ONLY:
			defer readFds[i].Close()
			go func() {
				defer io.Done()
				err := utils.ReadFrom(paths[i], writeFds[i])
				if err != nil {
					ioErr <- err
				}
			}()
		case WRITE_ONLY:
			defer writeFds[i].Close()
			go func() {
				defer io.Done()
				err := utils.WriteTo(readFds[i], paths[i], compression)
				if err != nil {
					ioErr <- err
				}
			}()
		}
	}

	args := []string{"--images-dir", dir}
	var extraFiles []*os.File

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

	cmd := exec.CommandContext(context.WithoutCancel(ctx), streamerBinary, args...)
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
			log.Trace().Str("context", "streamer").Str("dir", dir).Msg(scanner.Text())
		}
	}()

	err = cmd.Start()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start streamer: %w", err)
	}

	// embed OS FS, so methods we don't override are still available as normal FS operations in the dir
	fs = &Fs{mode, nil, afero.NewBasePathFs(afero.NewOsFs(), dir)}

	// Clean up on exit
	wg.Add(1)
	go func() {
		defer wg.Done()

		err := cmd.Wait()
		if err != nil {
			log.Trace().Err(err).Msg("streamer Wait()")
		}
		log.Trace().Int("code", cmd.ProcessState.ExitCode()).Msg("streamer exited")

		// FIXME: Remove socket files. Should be cleaned up by the streamer itself
		matches, err := filepath.Glob(filepath.Join(dir, "*.sock"))
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
	}

	// Connect to the streamer
	var conn net.Conn
	switch mode {
	case READ_ONLY:
		conn, err = net.Dial("unix", filepath.Join(dir, SERVE_SOCK))
	case WRITE_ONLY:
		conn, err = net.Dial("unix", filepath.Join(dir, CAPTURE_SOCK))
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to streamer: %w", err)
	}
	fs.conn = conn.(*net.UnixConn)
	log.Debug().Str("dir", dir).Msg("streamer connected")

	wait = func() error {
		// Stop the listener, and wait for all IO to finish
		fs.stopListener()
		fs.conn.Close()
		io.Wait()
		close(ioErr)
		return <-ioErr
	}

	return fs, wait, nil
}

func (fs *Fs) Create(name string) (afero.File, error) {
	rFd, wFd, err := fs.openFds(name)
	if err != nil {
		return nil, err
	}
	return &File{name: name, rFd: rFd, wFd: wFd}, nil
}

func (fs *Fs) Open(name string) (afero.File, error) {
	rFd, wFd, err := fs.openFds(name)
	if err != nil {
		return nil, err
	}

	return &File{name: name, rFd: rFd, wFd: wFd}, nil
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

func (fs *Fs) Chtimes(name string, atime, mtime time.Time) error {
	return fmt.Errorf("not implemented for streaming")
}

////////////////////
// Helper Methods //
////////////////////

// Opens a pair of file descriptors for reading and writing through the streamer
func (fs *Fs) openFds(name string) (int, int, error) {
	fds := make([]int, 2)
	err := syscall.Pipe(fds)
	if err != nil {
		return 0, 0, err
	}
	rFd, wFd := fds[0], fds[1]

	var streamerFd int

	switch fs.mode {
	case READ_ONLY:
		streamerFd = wFd // streamer should be able to write to this fd
	case WRITE_ONLY:
		streamerFd = rFd // streamer should be able to read from this fd
	default:
		return 0, 0, fmt.Errorf("invalid mode: %d", fs.mode)
	}

	// Send file request to streamer
	req := &img_streamer.ImgStreamerRequestEntry{Filename: name}
	data, err := proto.Marshal(req)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to marshal request: %w", err)
	}
	size := len(data)
	sizeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBuf, uint32(size))
	_, err = fs.conn.Write(sizeBuf)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to write size to file request: %w", err)
	}
	_, err = fs.conn.Write(data)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to write data to file request: %w", err)
	}
	sock, err := fs.conn.File()
	defer sock.Close()
	rights := syscall.UnixRights(streamerFd)
	err = syscall.Sendmsg(int(sock.Fd()), nil, rights, nil, 0)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to send streamer fd: %w", err)
	}

	return rFd, wFd, nil
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
	sizeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBuf, uint32(size))
	_, err = fs.conn.Write(sizeBuf)
	if err != nil {
		return fmt.Errorf("failed to write size to file request: %w", err)
	}
	_, err = fs.conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write data to file request: %w", err)
	}

	return nil
}

// Returns a list of image paths found in the image directory.
// Returns an error if the number of images found is not equal to the parallelism.
func imgPaths(dir string, mode Mode, parallelism int32) ([]string, error) {
	switch mode {
	case READ_ONLY:
		matches, err := filepath.Glob(filepath.Join(dir, IMG_FILE_PATTERN))
		if err != nil {
			return nil, err
		}
		if len(matches) != int(parallelism) {
			return nil, fmt.Errorf("expected %d images, got %d. please specify correct parallelism", parallelism, len(matches))
		}
		return matches, nil
	case WRITE_ONLY:
		paths := make([]string, parallelism)
		for i := range parallelism {
			paths[i] = filepath.Join(dir, fmt.Sprintf(IMG_FILE_FORMATTER, i))
		}
		return paths, nil
	default:
		return nil, fmt.Errorf("invalid mode: %d", mode)
	}
}
