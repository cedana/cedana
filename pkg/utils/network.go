package utils

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Checks if the given process has any active tcp connections
func HasActiveTCPConnections(pid int32) (bool, error) {
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
