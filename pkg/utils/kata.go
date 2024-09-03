package utils

import (
	"bytes"
	"os/exec"
	"strings"
	"errors"
	"strconv"
)

func ExtractCID(vm string) (uint32, error) {
	// Run the ps command to get process list
	psCmd := exec.Command("ps", "-aux")
	psOut, err := psCmd.Output()
	if err != nil {
		return 0, err
	}

	// Search for the specific qemu command
	searchTerm := "qemu-system-x86_64 -name sandbox-" + vm
	lines := bytes.Split(psOut, []byte("\n"))
	var matchedLine []byte
	for _, line := range lines {
		if bytes.Contains(line, []byte(searchTerm)) {
			matchedLine = line
			break
		}
	}

	if matchedLine == nil {
		return 0, errors.New("No lines matched")
	}

	// Extract guest-cid
	lineStr := string(matchedLine)
	parts := strings.Split(lineStr, "guest-cid=")
	if len(parts) < 2 {
		return 0, errors.New("No lines matched")
	}
	cidPart := strings.Fields(parts[1])[0]

	cid, err := strconv.Atoi(cidPart)
	if err != nil {
		return 0, err
	}

	return uint32(cid), nil
}
