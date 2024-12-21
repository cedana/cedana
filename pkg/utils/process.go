package utils

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/shirou/gopsutil/v4/process"
)

// CloseCommonFdscloses any common FDs between the parent and child process
func CloseCommonFds(ctx context.Context, parentPID, childPID int32) error {
	parent, err := process.NewProcessWithContext(ctx, parentPID)
	if err != nil {
		return err
	}

	child, err := process.NewProcessWithContext(ctx, childPID)
	if err != nil {
		return err
	}

	parentFds, err := parent.OpenFilesWithContext(ctx)
	if err != nil {
		return err
	}

	childFds, err := child.OpenFilesWithContext(ctx)
	if err != nil {
		return err
	}

	for _, pfd := range parentFds {
		for _, cfd := range childFds {
			if pfd.Path == cfd.Path && strings.Contains(pfd.Path, ".pid") {
				// we have a match, close the FD
				err := syscall.Close(int(cfd.Fd))
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func GetProcessState(ctx context.Context, pid uint32) (*daemon.ProcessState, error) {
	state := &daemon.ProcessState{}
	err := FillProcessState(ctx, pid, state)
	return state, err
}

func FillProcessState(ctx context.Context, pid uint32, state *daemon.ProcessState) error {
	if state == nil {
		return fmt.Errorf("state is nil")
	}
	state.PID = pid

	errs := []error{}

	p, err := process.NewProcessWithContext(ctx, int32(pid))
	if err != nil {
		return fmt.Errorf("could not get process: %v", err)
	}

	startTime, err := p.CreateTime()
	errs = append(errs, err)
	if err == nil {
		state.StartTime = uint64(startTime)
	}

	sid, _, errno := syscall.Syscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	if errno == 0 {
		state.SID = uint32(sid)
	}

	// get process uids, gids, and groups
	uids, err := p.UidsWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get uids: %v", err)
	}
	gids, err := p.GidsWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get gids: %v", err)
	}
	groups, err := p.GroupsWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get groups: %v", err)
	}
	state.UIDs = uids
	state.GIDs = gids
	state.Groups = groups

	var openFiles []*daemon.File
	of, err := p.OpenFilesWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get open files: %v", err)
	}
	for _, f := range of {
		var mode string
		file, err := os.Stat(f.Path)
		if err == nil {
			mode = file.Mode().Perm().String()
		}

		openFiles = append(openFiles, &daemon.File{
			Fd:   f.Fd,
			Path: f.Path,
			Mode: mode,
		})
	}

	// used for network barriers (TODO: NR)
	var openConnections []*daemon.Connection
	conns, err := p.ConnectionsWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get connections: %v", err)
	}
	for _, conn := range conns {
		Laddr := &daemon.Addr{
			IP:   conn.Laddr.IP,
			Port: conn.Laddr.Port,
		}
		Raddr := &daemon.Addr{
			IP:   conn.Raddr.IP,
			Port: conn.Raddr.Port,
		}
		openConnections = append(openConnections, &daemon.Connection{
			Fd:     conn.Fd,
			Family: conn.Family,
			Type:   conn.Type,
			Laddr:  Laddr,
			Raddr:  Raddr,
			Status: conn.Status,
			PID:    uint32(conn.Pid),
			UIDs:   conn.Uids,
		})
	}

	memoryUsed, err := p.MemoryPercentWithContext(ctx)
	errs = append(errs, err)
	proccessStatus, err := p.StatusWithContext(ctx)
	errs = append(errs, err)
	cwd, err := p.CwdWithContext(ctx)
	errs = append(errs, err)

	state.MemoryPercent = memoryUsed
	state.OpenFiles = openFiles
	state.OpenConnections = openConnections
	state.WorkingDir = cwd
	state.Status = strings.Join(proccessStatus, "")

	state.Host, err = GetHost(ctx)
	errs = append(errs, err)

	return errors.Join(errs...)
}

// Returns a channel that will be closed when a non-child process exits
// Since, we cannot use the process.Wait() method to wait for a non-child process to exit
// It waits until the process is a zombie process
func WaitForPid(pid uint32) chan int {
	exitCh := make(chan int)

	go func() {
		defer close(exitCh)
		for {
			// wait for the process to exit
			p, err := process.NewProcess(int32(pid))
			if err != nil {
				return
			}
			s, err := p.Status()
			if err != nil {
				return
			}
			for _, status := range s {
				if status == "zombie" {
					return
				}
			}
			time.Sleep(300 * time.Millisecond)
		}
	}()

	return exitCh
}

// Returns a channel that will be closed when a non-child process exits.
// Since, we cannot use the process.Wait() method to wait for a non-child process to exit.
// It waits until the process is a zombie process, or the process is not found.
func WaitForPidCtx(ctx context.Context, pid uint32) chan int {
	exitCh := make(chan int)

	go func() {
		defer close(exitCh)
		for {
			if ctx.Err() != nil {
				return
			}
			// wait for the process to exit
			p, err := process.NewProcessWithContext(ctx, int32(pid))
			if err != nil {
				return
			}
			s, err := p.Status()
			if err != nil {
				return
			}
			for _, status := range s {
				if status == "zombie" {
					return
				}
			}
			time.Sleep(300 * time.Millisecond)
		}
	}()

	return exitCh
}

// PidExists checks if a process with the given PID exists
func PidExists(pid uint32) bool {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return false
	}
	_, err = p.Status()
	if err != nil {
		return false
	}
	return true
}
