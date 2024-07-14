package api

// This file contains functions for managing process state in the DB for the service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cedana/cedana/api/services/task"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"

	"context"
)

const CHECKPOINT_STATE_FILE = "checkpoint_state.json"

func (s *service) updateState(ctx context.Context, jid string, state *task.ProcessState) error {
	marshalledState, err := json.Marshal(state)
	if err != nil {
		return err
	}

	// try creating the job, which would fail
	// in case the JID exists
	// On error, update the job
	err = s.db.PutJob(ctx, []byte(jid), marshalledState)
	return err
}

// Does not return an error if state is not found for a JID.
// Returns nil in that case
func (s *service) getState(ctx context.Context, jid string) (*task.ProcessState, error) {

	fetchedJob, err := s.db.GetJob(ctx, []byte(jid))
	if err != nil {
		return nil, err
	}

	state := task.ProcessState{}
	err = json.Unmarshal(fetchedJob.State, &state)
	if err != nil {
		return nil, err
	}
	return &state, err
}

// TODO NR - customizable errors
func (s *service) generateState(ctx context.Context, pid int32) (*task.ProcessState, error) {
	if pid == 0 {
		return nil, fmt.Errorf("invalid PID %d", pid)
	}

  state := &task.ProcessState{}

	p, err := process.NewProcessWithContext(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("could not get gopsutil process: %v", err)
	}

	state.PID = pid

	// Search for JID, if found, use that state with existing fields
	list, err := s.db.ListJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not list jobs: %v", err)
	}
	for _, job := range list {
		st := &task.ProcessState{}
		err = json.Unmarshal(job.State, st)
		if err != nil {
			continue
		}
		if st.PID == pid {
			state = st
			break
		}
	}

	var openFiles []*task.OpenFilesStat
	var openConnections []*task.ConnectionStat

	of, err := p.OpenFiles()
	for _, f := range of {
		var mode string
		var stream task.OpenFilesStat_StreamType
		file, err := os.Stat(f.Path)
		if err == nil {
			mode = file.Mode().Perm().String()
			switch f.Fd {
			case 0:
				stream = task.OpenFilesStat_STDIN
			case 1:
				stream = task.OpenFilesStat_STDOUT
			case 2:
				stream = task.OpenFilesStat_STDERR
			default:
				stream = task.OpenFilesStat_NONE
			}
		}

		openFiles = append(openFiles, &task.OpenFilesStat{
			Fd:     f.Fd,
			Path:   f.Path,
			Mode:   mode,
			Stream: stream,
		})
	}

	if err != nil {
		// don't want to error out and break
		return nil, nil
	}
	// used for network barriers (TODO: NR)
	conns, err := p.Connections()
	if err != nil {
		return nil, nil
	}
	for _, conn := range conns {
		Laddr := &task.Addr{
			IP:   conn.Laddr.IP,
			Port: conn.Laddr.Port,
		}
		Raddr := &task.Addr{
			IP:   conn.Raddr.IP,
			Port: conn.Raddr.Port,
		}
		openConnections = append(openConnections, &task.ConnectionStat{
			Fd:     conn.Fd,
			Family: conn.Family,
			Type:   conn.Type,
			Laddr:  Laddr,
			Raddr:  Raddr,
			Status: conn.Status,
			PID:    conn.Pid,
			UIDs:   conn.Uids,
		})
	}

	memoryUsed, _ := p.MemoryPercent()
	isRunning, _ := p.IsRunning()

	// if the process is actually running, we don't care that
	// we're potentially overriding a failed flag here.
	// In the case of a restored/resuscitated process this is a good thing

	// this is the status as returned by gopsutil.
	// ideally we want more than this, or some parsing to happen from this end
	status, _ := p.Status()

	// we need the cwd to ensure that it exists on the other side of the restore.
	// if it doesn't - we inheritFd it?
	cwd, err := p.Cwd()
	if err != nil {
		return nil, nil
	}

	// system information
	cpuinfo, _ := cpu.Info()
	vcpus, _ := cpu.Counts(true)
	state.CPUInfo = &task.CPUInfo{
		Count:      int32(vcpus),
		CPU:        cpuinfo[0].CPU,
		VendorID:   cpuinfo[0].VendorID,
		Family:     cpuinfo[0].Family,
		PhysicalID: cpuinfo[0].PhysicalID,
	}

	mem, _ := mem.VirtualMemory()
	state.MemoryInfo = &task.MemoryInfo{
		Total:     mem.Total,
		Available: mem.Available,
		Used:      mem.Used,
	}

	host, _ := host.Info()
	state.HostInfo = &task.HostInfo{
		HostID:               host.HostID,
		Hostname:             host.Hostname,
		OS:                   host.OS,
		Platform:             host.Platform,
		KernelVersion:        host.KernelVersion,
		KernelArch:           host.KernelArch,
		VirtualizationSystem: host.VirtualizationSystem,
		VirtualizationRole:   host.VirtualizationRole,
	}

	// ignore sending network for now, little complicated
	state.ProcessInfo = &task.ProcessInfo{
		OpenFds:         openFiles,
		WorkingDir:      cwd,
		MemoryPercent:   memoryUsed,
		IsRunning:       isRunning,
		OpenConnections: openConnections,
		Status:          strings.Join(status, ""),
	}

	return state, nil
}

func serializeStateToDir(dir string, state *task.ProcessState) error {
	serialized, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, CHECKPOINT_STATE_FILE)
	file, err := os.Create(path)
	if err != nil {
		return err
	}

	defer file.Close()
	_, err = file.Write(serialized)
	return err
}

func deserializeStateFromDir(dir string) (*task.ProcessState, error) {
	_, err := os.Stat(filepath.Join(dir, CHECKPOINT_STATE_FILE))
	if err != nil {
		return nil, fmt.Errorf("state file not found")
	}

	data, err := os.ReadFile(filepath.Join(dir, CHECKPOINT_STATE_FILE))
	if err != nil {
		return nil, fmt.Errorf("could not read state file: %v", err)
	}

	var checkpointState task.ProcessState
	err = json.Unmarshal(data, &checkpointState)
	return &checkpointState, err
}
