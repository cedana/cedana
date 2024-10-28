package utils

import (
	"net"
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

func GetMACAddress() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, i := range interfaces {
		// Skip loopback and interfaces without MAC addresses
		if i.Flags&net.FlagLoopback == 0 && i.HardwareAddr != nil {
			return i.HardwareAddr.String(), nil
		}
	}

	return "", nil
}
