package utils

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	TCP_ESTABLISHED = iota + 1
	TCP_SYN_SENT
	TCP_SYN_RECV
	TCP_FIN_WAIT1
	TCP_FIN_WAIT2
	TCP_TIME_WAIT
	TCP_CLOSE
	TCP_CLOSE_WAIT
	TCP_LAST_ACK
	TCP_LISTEN
	TCP_CLOSING
	TCP_NEW_SYN_RECV
)

func IsUsingIoUring(fdDir string) (bool, error) {

	// Read the contents of the fd directory
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return false, fmt.Errorf("failed to read fd directory: %v", err)
	}

	for _, fd := range fds {
		fdPath := filepath.Join(fdDir, fd.Name())

		// Read the symbolic link to see where it points
		linkTarget, err := os.Readlink(fdPath)
		if err != nil {
			return false, fmt.Errorf("failed to read link target for %s: %v", fdPath, err)
		}

		// Check if the link points to io_uring
		if strings.Contains(linkTarget, "anon_inode:[io_uring]") {
			return true, nil
		}
	}

	return false, nil
}

func GetTCPStates(reader io.Reader) ([]uint64, error) {
	var states []uint64
	scanner := bufio.NewScanner(reader)

	// Skip the header line
	if !scanner.Scan() {
		return nil, scanner.Err()
	}

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		// Ensure that we have enough fields
		if len(fields) > 3 {
			stateStr := fields[3]
			stateInt, err := strconv.ParseUint(stateStr, 16, 32) // Convert hex string to uint32
			if err != nil {
				return nil, err
			}
			states = append(states, stateInt)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return states, nil
}

func IsReadyLoop(getTCPStates func(io.Reader) ([]uint64, error), getTCPReader func() (io.Reader, error), IsUsingIoUring func(fdDir string) (bool, error), iteration int, timeoutInMs time.Duration, fdDir string) (bool, error) {
	isReady := true
	for i := 0; i < iteration; i++ {
		reader, err := getTCPReader()
		if err != nil {
			return false, err
		}

		tcpStates, err := getTCPStates(reader)
		if err != nil {
			return false, err
		}

		isReady = true
		for _, state := range tcpStates {
			if state == TCP_SYN_RECV || state == TCP_SYN_SENT {
				isReady = false
				break
			}
		}

		isUsingIoUring := false

		if IsUsingIoUring != nil {
			isUsingIoUring, err = IsUsingIoUring(fdDir)
			if err != nil {
				return false, err
			}
		}

		if isUsingIoUring {
			isReady = false
		}

		if isReady {
			break
		}

		if i < iteration-1 {
			time.Sleep(timeoutInMs * time.Millisecond)
		}
	}

	return isReady, nil
}
