package utils

import (
	"net"
	"os"
	"strings"
)

type Machine struct {
	ID       string
	MACAddr  string
	Hostname string
}

func GetMachine() (Machine, error) {
	machine := Machine{}

	machineID, err := GetMachineID()
	if err != nil {
		return machine, err
	}
	machine.ID = machineID

	macAddr, err := GetMACAddress()
	if err != nil {
		return machine, err
	}
	machine.MACAddr = macAddr

	hostname, err := os.Hostname()
	if err != nil {
		return machine, err
	}
	machine.Hostname = hostname

	return machine, nil
}

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
