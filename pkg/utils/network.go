package utils

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Checks if the given process has any active tcp connections
func HasActiveTCPConnections(pid uint32) (bool, error) {
	tcpFile := filepath.Join("/proc", fmt.Sprintf("%d", pid), "net/tcp")

	file, err := os.Open(tcpFile)
	if err != nil {
		return false, fmt.Errorf("failed to open %s: %v", tcpFile, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "  sl") {
			continue
		}
		return true, nil
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("error reading %s: %v", tcpFile, err)
	}

	return false, nil
}

// GetFreePort returns a free random port on the host
func GetFreePort() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	addr := listener.Addr().String()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	portInt, err := strconv.Atoi(port)
	if err != nil {
		return 0, err
	}
	return portInt, nil
}
