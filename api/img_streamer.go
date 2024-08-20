package api

import (
	"encoding/binary"
	"fmt"
	img_streamer "github.com/cedana/cedana/api/services/img_streamer"
	"google.golang.org/protobuf/proto"
	"net"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
)

const (
	IMG_STREAMER_CAPTURE_SOCKET_NAME = "ced-capture.sock"
	IMG_STREAMER_SERVE_SOCKET_NAME   = "ced-serve.sock"
	O_DUMP                           = 577
	O_RSTR                           = 578
)

var (
	imgStreamerFdLock sync.Mutex
	imgStreamerMode   int
)

func socketNameForMode(mode int) string {
	switch mode {
	case O_DUMP:
		return IMG_STREAMER_CAPTURE_SOCKET_NAME
	case O_RSTR:
		return IMG_STREAMER_SERVE_SOCKET_NAME
	default:
		panic("BUG")
	}
}

func imgStreamerInit(imageDir string, mode int) (*net.UnixConn, error) {
	imgStreamerMode = mode
	socketPath := filepath.Join(imageDir, socketNameForMode(mode))
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to image streamer socket %s: %v", socketPath, err)
	}
	return conn.(*net.UnixConn), nil
}

func imgStreamerFinish(socket_fd int, criu_fd int, streamer_fd int) {
	syscall.Close(criu_fd)
	syscall.Close(streamer_fd)
	syscall.Close(socket_fd)
}

func sendFileRequest(filename string, conn *net.UnixConn, r_fd int) (int, error) {
	req := &img_streamer.ImgStreamerRequestEntry{Filename: filename}
	data, err := proto.Marshal(req)
	size := uint32(len(data))
	sizeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBuf, size)
	if _, err := conn.Write(sizeBuf); err != nil {
		return 0, fmt.Errorf("failed to write sizeBuf %v: %v", sizeBuf, err)
	}
	if _, err := conn.Write(data); err != nil {
		return 0, fmt.Errorf("failed to write filename %s: %v", filename, err)
	}
	socket, err := conn.File()
	socket_fd := int(socket.Fd())
	rights := syscall.UnixRights(r_fd)

	if err = syscall.Sendmsg(socket_fd, nil, rights, nil, 0); err != nil {
		return 0, fmt.Errorf("Failed to send file descriptor with rights: %v", err)
	}
	return socket_fd, nil
}

func establishStreamerFilePipe() (int, int, error) {
	fds := make([]int, 2)
	err := syscall.Pipe(fds)
	if err != nil {
		return -1, -1, fmt.Errorf("Unable to create pipe with fds %v: %v", fds, err)
	}
	return fds[0], fds[1], nil // r,w,nil
}

func _imgStreamerOpen(filename string, conn *net.UnixConn) (int, int, int, error) {
	signal.Ignore(syscall.SIGPIPE)
	r_fd, w_fd, err := establishStreamerFilePipe()

	var open_fd int
	if imgStreamerMode == O_DUMP {
		open_fd = r_fd
	} else if imgStreamerMode == O_RSTR {
		open_fd = w_fd
	} else {
		return -1, -1, -1, fmt.Errorf("Unknown imgStreamerMode %s", imgStreamerMode)
	}

	socket_fd, err := sendFileRequest(filename, conn, open_fd)
	if err != nil {
		return -1, -1, -1, err
	}

	return socket_fd, r_fd, w_fd, err
}

func imgStreamerOpen(filename string, conn *net.UnixConn) (int, int, int, error) {
	imgStreamerFdLock.Lock()
	defer imgStreamerFdLock.Unlock()

	socket_fd, r_fd, w_fd, err := _imgStreamerOpen(filename, conn)
	return socket_fd, r_fd, w_fd, err
}
