package cedana

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog/log"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
)

func (s *Server) CreateUnixSocket(ctx context.Context, _ *daemon.Empty) (*daemon.SocketResp, error) {
	tempDir := os.TempDir()
	socketPath := filepath.Join(tempDir, fmt.Sprintf("ced_fdsock_%d.sock", os.Getpid()))
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create Unix socket: %w", err)
	}

	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				fmt.Printf("Connection error: %v\n", err)
				break
			}

			go s.handleConnection(conn, socketPath)
		}
	}()

	return &daemon.SocketResp{SocketPath: socketPath}, nil
}

func (s *Server) handleConnection(conn net.Conn, socketPath string) {
	defer conn.Close()

	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		log.Logger.Warn().Msgf("Not a Unix connection")
		return
	}

	// Read file descriptors
	oob := make([]byte, syscall.CmsgSpace(4*4)) // Space for up to 4 FDs
	buf := make([]byte, 1024)
	n, oobn, _, _, err := unixConn.ReadMsgUnix(buf, oob)
	if err != nil {
		log.Logger.Warn().Msgf("Failed to read message: %v\n", err)
		return
	}

	cmsgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		log.Logger.Warn().Msgf("Failed to parse control messages: %v\n", err)
		return
	}

	var fds []int
	for _, cmsg := range cmsgs {
		fdArr, err := syscall.ParseUnixRights(&cmsg)
		if err != nil {
			log.Logger.Warn().Msgf("Failed to parse Unix rights: %v\n", err)
			return
		}
		fds = append(fds, fdArr...)
	}

	log.Debug().Msgf("Received FDs: %v, message: %s\n", fds, string(buf[:n]))

	requestID := string(buf[:n]) // Assume request ID is sent with the message
	s.fdStore.Store(requestID, fds)

	defer os.Remove(socketPath)

}
