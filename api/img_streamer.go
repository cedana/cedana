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
	IMG_STREAMER_CAPTURE_SOCKET_NAME = "streamer-capture.sock"
	IMG_STREAMER_SERVE_SOCKET_NAME   = "streamer-serve.sock"
	O_DUMP                           = 577
	O_RSTR                           = 578
	READ_PIPE                        = 0
	WRITE_PIPE                       = 1
)

var (
	imgStreamerFdLock sync.Mutex
	imgStreamerMode   int
	conn              net.Conn
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

func (s *service) imgStreamerInit(imageDir string, mode int) (net.Conn, error) {
	imgStreamerMode = mode
	socketPath := filepath.Join(imageDir, socketNameForMode(mode))
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		s.logger.Warn().Msgf("unable to connect to image streamer socket: %s", socketPath)
		return nil, err
	} else {
		s.logger.Info().Msgf("able to connect to image streamer socket %s", socketPath)
	}
	return conn, nil
}

func (s *service) imgStreamerFinish(socket_fd int, criu_fd int, streamer_fd int) {
	s.logger.Info().Msgf("entered imgStreamerFinish")
	syscall.Close(criu_fd)
	s.logger.Info().Msgf("closed criu_fd = %d", criu_fd)
	syscall.Close(streamer_fd)
	s.logger.Info().Msgf("closed streamer_fd = %d", streamer_fd)
	syscall.Close(socket_fd)
	s.logger.Info().Msgf("closed socket_fd = %d", socket_fd)
}

func (s *service) sendFileRequest(filename string, conn net.Conn) (int, error) {
	s.logger.Info().Msgf("entered sendFileRequest")
	req := &img_streamer.ImgStreamerRequestEntry{Filename: filename}
	data, err := proto.Marshal(req)
	size := uint32(len(data))
	sizeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBuf, size)
	if _, err := conn.Write(sizeBuf); err != nil {
		s.logger.Warn().Msgf("failed to write sizeBuf %v", sizeBuf)
	} else {
		s.logger.Info().Msgf("wrote sizeBuf %v", sizeBuf)
	}
	if _, err := conn.Write(data); err != nil {
		s.logger.Warn().Msgf("failed to write filename=checkpoint_state.json")
		return -1, nil // err
	} else {
		s.logger.Info().Msgf("wrote filename=checkpoint_state.json as serialized: %v", data)
	}
	socket, err := conn.(*net.UnixConn).File()
	socket_fd := int(socket.Fd())
	s.logger.Info().Msgf("socket fd = %d", socket_fd)
	signal.Ignore(syscall.SIGPIPE)
	r_fd, w_fd, err := s.establishStreamerFilePipe()
	if err != nil {
		s.logger.Warn().Msgf("establishStreamerFilePipe failed with err %v", err)
	} else {
		s.logger.Info().Msgf("establishStreamerFilePipe succeeded with r_fd %v, w_fd %v", r_fd, w_fd)
	}
	rights := syscall.UnixRights(r_fd)

	if err = syscall.Sendmsg(socket_fd, nil, rights, nil, 0); err != nil {
		s.logger.Warn().Msgf("failed to send file descriptor with rights %v: %v", rights, err)
	} else {
		s.logger.Info().Msgf("sent file descriptor using rights %v", rights)
	}
	return socket_fd, nil
}

func (s *service) recvFileReply(exists *bool) error {
	s.logger.Info().Msgf("entered recvFileReply(exists = %v)", exists)
	var reply *img_streamer.ImgStreamerReplyEntry
	var err error
	err = nil // pbReadOneFd(fd, &reply, PB_IMG_STREAMER_REPLY) - TODO
	if err != nil {
		return err
	}

	*exists = reply.Exists
	return nil
}

func (s *service) establishStreamerFilePipe() (int, int, error) {
	s.logger.Info().Msgf("entered establishStreamerFilePipe")
	fds := make([]int, 2)
	err := syscall.Pipe(fds)
	if err != nil {
		s.logger.Warn().Msgf("unable to create pipe with fds %v, error %v", fds, err)
		return -1, -1, fmt.Errorf("unable to create pipe: %w", err)
	} else {
		s.logger.Info().Msgf("successfully created pipe with fds %v", fds)
	}
	return fds[0], fds[1], nil // r,w,nil
}

func (s *service) _imgStreamerOpen(filename string, conn net.Conn) (int, int, int, error) {
	s.logger.Info().Msgf("entered _imgStreamerOpen")
	socket_fd, err := s.sendFileRequest(filename, conn)
	if err != nil {
		return -1, -1, -1, err
	}

	if imgStreamerMode == O_RSTR {
		var exists bool
		err = s.recvFileReply(&exists)
		if err != nil {
			return -1, -1, -1, err
		}

		if !exists {
			return -1, -1, -1, nil
		}
	}

	r_fd, w_fd, err := s.establishStreamerFilePipe()
	return socket_fd, r_fd, w_fd, err
}

func (s *service) imgStreamerOpen(filename string, conn net.Conn) (int, int, int, error) {
	s.logger.Info().Msgf("entered imgStreamerOpen(filename=%s)", filename)

	imgStreamerFdLock.Lock()
	defer imgStreamerFdLock.Unlock()

	socket_fd, r_fd, w_fd, err := s._imgStreamerOpen(filename, conn)
	s.logger.Info().Msgf("imgStreamerOpen returned r_fd=%v, w_fd=%v, socket_fd=%v", r_fd, w_fd, socket_fd)
	return socket_fd, r_fd, w_fd, err
}
