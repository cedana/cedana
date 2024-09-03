package utils

import (
	"os"
	"strings"
)

func GetMachineID() (string, error) {
	// read from /etc/machine-id
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return "", err
	}

	machineID := string(data)
	machineID = strings.TrimSpace(machineID)

	return machineID, nil
}
