package utils

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	TCP_ESTABLISHED = iota
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

func IsTCPReady(getTCPStates func(io.Reader) ([]uint64, error), getReader func() (io.Reader, error), iteration int, timeoutInMs time.Duration) (bool, error) {
	isReady := true
	for i := 0; i < iteration; i++ {
		reader, err := getReader()
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

		if isReady {
			break
		}

		if i < iteration-1 {
			time.Sleep(timeoutInMs * time.Millisecond)
		}
	}

	return isReady, nil
}
