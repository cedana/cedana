package server

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cedana/cedana/api/services/task"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/spf13/afero"
)

// WriteOnlyFds takes a snapshot of files that are open (in writeonly) by process PID
// and outputs full paths. For concurrent processes (multithreaded) this can be dangerous and lead to
// weird race conditions (maybe).
// To avoid actually using ptrace (TODO NR) we loop through the openFds of the process and check the
// flags.
func WriteOnlyFds(openFds []*task.OpenFilesStat, pid int32) []string {
	fs := &afero.Afero{Fs: afero.NewOsFs()}
	var paths []string
	for _, fd := range openFds {
		info, err := fs.ReadFile(fmt.Sprintf("/proc/%s/fdinfo/%s", strconv.Itoa(int(pid)), strconv.FormatUint(fd.Fd, 10)))
		if err != nil {
			// c.Logger.Debug().Msgf("could not read fdinfo: %v", err)
			continue
		}

		lines := strings.Split(string(info), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "flags:") {
				// parsing out flags from the line and converting it out of octal.
				// so converting flags: 0100002 -> 32770
				flags, err := strconv.ParseInt(strings.TrimSpace(line[6:]), 8, 0)
				if err != nil {
					// c.Logger.Debug().Msgf("could not parse flags: %v", err)
					continue
				}

				// bitwise compare flags with os.O_RDWR
				if int(flags)&os.O_RDWR != 0 || int(flags)&os.O_WRONLY != 0 {
					// gopsutil appends a (deleted) flag to the path sometimes, which I'm not fully sure of why yet
					// TODO NR - figure this out
					path := strings.Replace(fd.Path, " (deleted)", "", -1)
					paths = append(paths, path)
				}
			}
		}
	}
	return paths
}

func getState(pid int32) *task.ClientStateStreamingArgs {

	if pid == 0 {
		return nil
	}

	p, err := process.NewProcess(pid)
	if err != nil {
		// c.Logger.Info().Msgf("Could not instantiate new gopsutil process with error %v", err)
	}

	var openFiles []*task.OpenFilesStat
	var writeOnlyFiles []string
	var openConnections []*task.ConnectionStat
	var flag task.FlagEnum

	if p != nil {
		openFilesOrig, err := p.OpenFiles()
		for _, f := range openFilesOrig {
			openFiles = append(openFiles, &task.OpenFilesStat{
				Fd:   f.Fd,
				Path: f.Path,
			})
		}

		if err != nil {
			// don't want to error out and break
			return nil
		}
		// used for network barriers (TODO: NR)
		openConnectionsOrig, err := p.Connections()
		if err != nil {
			return nil
		}
		for _, c := range openConnectionsOrig {
			Laddr := &task.Addr{
				IP:   c.Laddr.IP,
				Port: c.Laddr.Port,
			}
			Raddr := &task.Addr{
				IP:   c.Raddr.IP,
				Port: c.Raddr.Port,
			}
			openConnections = append(openConnections, &task.ConnectionStat{
				Fd:     c.Fd,
				Family: c.Family,
				Type:   c.Type,
				Laddr:  Laddr,
				Raddr:  Raddr,
				Status: c.Status,
				Pid:    c.Pid,
				Uids:   c.Uids,
			})
		}

		writeOnlyFiles = WriteOnlyFds(openFiles, pid)
	}

	memoryUsed, _ := p.MemoryPercent()
	isRunning, _ := p.IsRunning()

	// if the process is actually running, we don't care that
	// we're potentially overriding a failed flag here.
	// In the case of a restored/resuscitated process this is a good thing
	if isRunning {
		flag = task.FlagEnum_JOB_RUNNING
	}

	// this is the status as returned by gopsutil.
	// ideally we want more than this, or some parsing to happen from this end
	status, _ := p.Status()

	m, _ := mem.VirtualMemory()
	h, _ := host.Info()

	// ignore sending network for now, little complicated
	return &task.ClientStateStreamingArgs{
		ProcessInfo: &task.ProcessInfo{
			PID:                    pid,
			OpenFds:                openFiles,
			OpenWriteOnlyFilePaths: writeOnlyFiles,
			MemoryPercent:          memoryUsed,
			IsRunning:              isRunning,
			OpenConnections:        openConnections,
			Status:                 strings.Join(status, ""),
		},
		ClientInfo: &task.ClientInfo{
			Id:              "NOT IMPLEMENTED",
			Hostname:        h.Hostname,
			Platform:        h.Platform,
			OS:              h.OS,
			Uptime:          h.Uptime,
			RemainingMemory: m.Available,
		},
		Flag:            flag,
		CheckpointState: "NOT IMPLEMENTED",
	}
}
