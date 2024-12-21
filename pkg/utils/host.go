package utils

import (
	"context"
	"errors"
	"fmt"
	"net"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

func GetHost(ctx context.Context) (*daemon.Host, error) {
	state := &daemon.Host{}
	err := FillHost(ctx, state)
	return state, err
}

func FillHost(ctx context.Context, state *daemon.Host) error {
	if state == nil {
		return fmt.Errorf("state is nil")
	}

	errs := []error{}

	cpuinfo, err := cpu.InfoWithContext(ctx)
	errs = append(errs, err)
	vcpus, err := cpu.CountsWithContext(ctx, true)
	errs = append(errs, err)
	if err == nil {
		state.CPU = &daemon.CPU{
			Count:      int32(vcpus),
			CPU:        cpuinfo[0].CPU,
			VendorID:   cpuinfo[0].VendorID,
			Family:     cpuinfo[0].Family,
			PhysicalID: cpuinfo[0].PhysicalID,
		}
	}

	mem, err := mem.VirtualMemoryWithContext(ctx)
	errs = append(errs, err)
	if err == nil {
		state.Memory = &daemon.Memory{
			Total:     mem.Total,
			Available: mem.Available,
			Used:      mem.Used,
		}
	}

	info, err := host.InfoWithContext(ctx)
	errs = append(errs, err)
	if err == nil {
		state.ID = info.HostID
		state.Hostname = info.Hostname
		state.OS = info.OS
		state.Platform = info.Platform
		state.KernelVersion = info.KernelVersion
		state.KernelArch = info.KernelArch
		state.VirtualizationSystem = info.VirtualizationSystem
		state.VirtualizationRole = info.VirtualizationRole
	}

	mac, err := GetMACAddress()
	errs = append(errs, err)
	if err == nil {
		state.MAC = mac
	}

	return errors.Join(errs...)
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
