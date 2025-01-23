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
	"strconv"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana-image-streamer/protocolbuffers/go/img_streamer"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"google.golang.org/protobuf/proto"
)

type Mode int

const (
	READ_ONLY Mode = iota
	WRITE_ONLY
)
const DEFAULT_PARALLELISM = 2

const (
	CAPTURE_SOCK = "ced-capture.sock"
	SERVE_SOCK   = "ced-serve.sock"
)

// Implementation of the afero.Fs filesystem interface that uses streaming as the backend
// using the streamer plugin.
type Fs struct {
	mode Mode
	conn *net.UnixConn
	afero.Fs
}

func NewStreamingFs(ctx context.Context, wg *sync.WaitGroup, streamerBinary string, dir string, parallelism int32, mode Mode) (*Fs, error) {
	// Start the image streamer based on the provided mode

	if parallelism < 1 {
		parallelism = DEFAULT_PARALLELISM
	}

	args := []string{"--dir", dir, "--num-pipes", strconv.Itoa(int(parallelism))}

	switch mode {
	case READ_ONLY:
		args = append(args, "serve")
	case WRITE_ONLY:
		args = append(args, "capture")
	default:
		return nil, fmt.Errorf("invalid mode: %d", mode)
	}

	logger := logging.Writer("streamer", dir, zerolog.TraceLevel)

	cmd := exec.CommandContext(ctx, streamerBinary, args...)
	cmd.Stdout = logger
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) } // AVOID SIGKILL
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	ready := make(chan bool, 1)

	// Wait on stderr for readiness
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(ready)

		bufio.NewReader(stderrPipe).ReadString('\n') // just wait for the first line/err
	}()

	// embed OS FS, so methods we don't override are still available as normal FS operations in the dir
	fs := &Fs{mode, nil, afero.NewBasePathFs(afero.NewOsFs(), dir)}

	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start streamer: %w", err)
	}

	// Clean up on exit
	wg.Add(1)
	go func() {
		defer wg.Done()
		if fs.conn != nil {
			defer fs.conn.Close()
		}

		err := cmd.Wait()
		if err != nil {
			log.Trace().Err(err).Msg("streamer Wait()")
		}
		log.Debug().Int("code", cmd.ProcessState.ExitCode()).Msg("streamer exited")

		// FIXME: Remove socket files. Should be cleaned up by the streamer itself (?)
		matches, err := filepath.Glob(filepath.Join(dir, "*.sock"))
		if err == nil {
			for _, match := range matches {
				os.Remove(match)
			}
		}
	}()

	<-ready

	// Connect to the streamer
	var conn net.Conn
	switch mode {
	case READ_ONLY:
		conn, err = net.Dial("unix", filepath.Join(dir, SERVE_SOCK))
	case WRITE_ONLY:
		conn, err = net.Dial("unix", filepath.Join(dir, CAPTURE_SOCK))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect to streamer: %w", err)
	}

	fs.conn = conn.(*net.UnixConn)

	return fs, nil
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
