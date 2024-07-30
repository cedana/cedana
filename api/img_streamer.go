package api

import (
	"encoding/binary"
	//"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"syscall"
	"google.golang.org/protobuf/proto"
)

const (
	IMG_STREAMER_CAPTURE_SOCKET_NAME = "streamer-capture.sock"
	IMG_STREAMER_SERVE_SOCKET_NAME   = "streamer-serve.sock"
	O_DUMP                           = 1
	O_RSTR                           = 2
	IMG_STREAMER_FD_OFF              = 3
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

type xbuf struct {
	mem  string /* buffer */
	data string /* position we see bytes at */
	sz   uint   /* bytes sitting after b->pos */
}
type bfd struct {
	fd       uintptr
	writable bool
	b        xbuf
}
type cr_img struct {
	_x bfd
}

func (s *service) imgStreamerInit(imageDir string, mode int, fd uintptr) error {

	socketPath := filepath.Join(imageDir, socketNameForMode(mode))
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("unable to connect to image streamer socket: %s", socketPath)
	} else {
		s.logger.Info().Msgf("able to connect to image streamer socket %s", socketPath)
	}
	req := &ImgStreamerRequestEntry{Filename: "checkpoint_state.json"}
	data, err := proto.Marshal(req) // writes through to criu-image-streamer with json.Marshal
	if err != nil {
		s.logger.Warn().Msgf("Failed to marshal req %v: %v", req, err)
	}
	size := uint32(len(data))
	sizeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBuf, size)
	if _, err := conn.Write(sizeBuf); err != nil {
    s.logger.Warn().Msgf("failed to write sizeBuf %v", sizeBuf)
	} else {
		s.logger.Info().Msgf("wrote sizeBuf %v", sizeBuf)
	}
	/*out := make([]byte, 1024)
	    copy(out, data)
	    iovec := []syscall.Iovec{
	  		{
	  			Base: (*byte)(unsafe.Pointer(&size)),
	  			Len:  uint64(unsafe.Sizeof(size)),
	  		},
	      {
	    			Base: &out[0],
	  			Len:  uint64(len(out)),
	  		},
	  	}*/

	//nw, err := vectorio.WritevRaw(fd,iovec)
	/*if (err != nil) {
	    s.logger.Warn().Msgf("WritevRaw(fd=%v,iovec=%v) failed",fd,iovec)
	  } else {
	    s.logger.Info().Msgf("WritevRaw(fd=%v,iovec=%v) succeeded: wrote %v bytes",fd,iovec,nw)

	  }*/
	s.logger.Info().Msgf("done writing to fd, going to try writing to conn instead")
	if _, err := conn.Write(data); err != nil {
		s.logger.Warn().Msgf("failed to write checkpoint_state.json")
		return nil // err
	} else {
		s.logger.Info().Msgf("wrote %v to checkpoint_state.json", data)
	}
	conn.Close()
	return nil
}

func (s *service) imgStreamerFinish() {
	s.logger.Info().Msgf("entered imgStreamerFinish")
	if getServiceFd(IMG_STREAMER_FD_OFF) >= 0 {
		fmt.Println("Dismissing the image streamer")
		closeServiceFd(IMG_STREAMER_FD_OFF)
	}
}

func (s *service) pbWriteOneFd(fd uintptr, obj interface{}, objType int) error {
	s.logger.Info().Msgf("entered pbWriteOneFd(fd = %v, obj = %v, objType = %d)", fd, obj, objType)
	img := &cr_img{_x: bfd{fd: fd}}
	err := s.pbWriteOne(img, obj, objType)
	if err != nil {
		return fmt.Errorf("failed to communicate with the image streamer: %w", err)
	} else {
		s.logger.Info().Msgf("succeeded in pbWriteOne")
	}
	return nil
}

func pbReadOneFd(fd uintptr, pobj interface{}, objType int) error {
	img := &cr_img{_x: bfd{fd: fd}}
	err := pbReadOne(img, pobj, objType)
	if err != nil {
		return fmt.Errorf("failed to communicate with the image streamer: %w", err)
	}
	return nil
}

func (s *service) sendFileRequest(filename string, fd uintptr) error {
	s.logger.Info().Msgf("entered sendFileRequest")
	req := &ImgStreamerRequestEntry{Filename: filename}

	return s.pbWriteOneFd(fd, req, PB_IMG_STREAMER_REQUEST)
}

func (s *service) recvFileReply(exists *bool, fd uintptr) error {
	s.logger.Info().Msgf("entered recvFileReply(exists = %v, fd = %v)", exists, fd)
	var reply *ImgStreamerReplyEntry
	err := pbReadOneFd(fd, &reply, PB_IMG_STREAMER_REPLY)
	if err != nil {
		return err
	}

	*exists = reply.Exists
	return nil
}

func (s *service) establishStreamerFilePipe() (int, error) {
	s.logger.Info().Msgf("entered establishStreamerFilePipe")
	var criuPipeDirection, streamerPipeDirection int
	if imgStreamerMode == O_DUMP {
		criuPipeDirection = syscall.O_WRONLY
		streamerPipeDirection = syscall.O_RDONLY
	} else {
		criuPipeDirection = syscall.O_RDONLY
		streamerPipeDirection = syscall.O_WRONLY
	}

	fds := make([]int, 2)
	err := syscall.Pipe(fds)
	if err != nil {
		return -1, fmt.Errorf("unable to create pipe: %w", err)
	}

	err = sendFd(getServiceFd(IMG_STREAMER_FD_OFF), nil, 0, fds[streamerPipeDirection])
	if err != nil {
		syscall.Close(fds[criuPipeDirection])
		return -1, err
	}

	syscall.Close(fds[streamerPipeDirection])
	return fds[criuPipeDirection], nil
}

func (s *service) _imgStreamerOpen(filename string, fd uintptr) (int, error) {
	s.logger.Info().Msgf("entered _imgStreamerOpen")
	err := s.sendFileRequest(filename, fd)
	if err != nil {
		return -1, err
	}

	if imgStreamerMode == O_RSTR {
		var exists bool
		err = s.recvFileReply(&exists, fd)
		if err != nil {
			return -1, err
		}

		if !exists {
			return -1, nil
		}
	}

	return s.establishStreamerFilePipe()
}

func (s *service) imgStreamerOpen(filename string, flags int, fd uintptr) (int, error) {
	s.logger.Info().Msgf("entered imgStreamerOpen(filename=%s,flags=%d,fd=%v)", filename, flags, fd)
	/*if flags != imgStreamerMode {
	    s.logger.Warn().Msgf("flags != imgStreamerMode(%v)",imgStreamerMode)
			panic("BUG")
		} else {
	    s.logger.Info().Msgf("flags = imgStreamerMode")
	  }*/

	imgStreamerFdLock.Lock()
	defer imgStreamerFdLock.Unlock()

	fd_, err := s._imgStreamerOpen(filename, fd)
	return fd_, err
}

func installServiceFd(offset int, conn net.Conn) error {
	// Implementation of installing service FD
	// This is a placeholder and needs to be implemented
	return nil
}

func getServiceFd(offset int) int {
	// Implementation of getting service FD
	// This is a placeholder and needs to be implemented
	return -1
}

func closeServiceFd(offset int) {
	// Implementation of closing service FD
	// This is a placeholder and needs to be implemented
}

func sendFd(serviceFd int, data []byte, flags int, fd int) error {
	// Implementation of sending file descriptor
	// This is a placeholder and needs to be implemented
	return nil
}

/*type crImg struct {
	fd uintptr
}*/

func (s *service) pbWriteOne(img *cr_img, obj interface{}, objType int) error {
	/*data, err := json.Marshal(obj)
	if err != nil {
		s.logger.Warn().Msgf("Failed to serialize protobuf message: %v\n", err)
		return err
	} else {
		s.logger.Info().Msgf("Serialized protobuf message %v to data = %v\n", obj, data)

	}

	s.logger.Info().Msgf("len(data) = %d, uint32(len(data)) = %d", len(data), uint32(len(data)))
	size := uint32(len(data))
	s.logger.Info().Msgf("Serialized protobuf message size = %v", size)
	sizeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBuf, size)
	s.logger.Info().Msgf("sizeBuf = %v", sizeBuf)*/

	/*if _, err := conn.Write(sizeBuf); err != nil {
		s.logger.Warn().Msgf("Failed to write message size: %v", err)
		return err
	} else {
		s.logger.Info().Msgf("wrote message size / why ?")
	} */

	/*if _, err := conn.Write([]byte("m")); err != nil { //checkpoint_state.json")); err != nil {
	    s.logger.Warn().Msgf("failed to write checkpoint_state.json")
	    return nil //err
	  } else {
	    s.logger.Info().Msgf("wrote checkpoint_state.json")
	  }*/
	//s.logger.Info().Msgf("not doing anything because failure !")
	/*if _, err := conn.Write(data); err != nil {
	    s.logger.Warn().Msgf("failed to write data")
	    return err
	  } else {
	    s.logger.Info().Msgf("wrote data")
	  }*/
	return nil

	//unix.Writev(fd,[][]byte{objType, obj} )
}

func pbReadOne(img *cr_img, pobj interface{}, objType int) error {
	// Implementation of reading protobuf data
	// This is a placeholder and needs to be implemented
	return nil
}

type ImgStreamerRequestEntry struct {
	Filename string
}

type ImgStreamerReplyEntry struct {
	Exists bool
}

const (
	PB_IMG_STREAMER_REQUEST = 1
	PB_IMG_STREAMER_REPLY   = 2
)
