package ptrace

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

const (
	stopPollInterval = time.Millisecond
	stableScans      = 2
)

type Held struct {
	reqs        chan func()
	released    chan struct{}
	releaseOnce sync.Once
	tids        map[int]unix.Signal
	skipped     int
}

func Hold(list func() ([]int, error), skip func(pid int) bool, timeout time.Duration) (*Held, error) {
	h := &Held{
		reqs:     make(chan func()),
		released: make(chan struct{}),
		tids:     make(map[int]unix.Signal),
	}

	errCh := make(chan error, 1)
	go h.tracer(func() { errCh <- h.seize(list, skip, time.Now().Add(timeout)) })

	if err := <-errCh; err != nil {
		h.Release()
		return nil, err
	}
	return h, nil
}

func (h *Held) Len() int {
	n := 0
	h.do(func() { n = len(h.tids) })
	return n
}

func (h *Held) Skipped() int {
	n := 0
	h.do(func() { n = h.skipped })
	return n
}

func (h *Held) Release() {
	h.do(func() {
		h.releaseOnce.Do(func() {
			for tid, sig := range h.tids {
				release(tid, sig)
			}
			h.tids = map[int]unix.Signal{}
			close(h.released)
		})
	})
}

func (h *Held) tracer(first func()) {
	runtime.LockOSThread()
	first()
	for {
		select {
		case req := <-h.reqs:
			req()
		case <-h.released:
			return
		}
	}
}

func (h *Held) do(f func()) {
	done := make(chan struct{})
	select {
	case h.reqs <- func() { f(); close(done) }:
		<-done
	case <-h.released:
	}
}

func (h *Held) seize(list func() ([]int, error), skip func(pid int) bool, deadline time.Time) error {
	skipped := map[int]bool{}

	for quiet := 0; quiet < stableScans; {
		if time.Now().After(deadline) {
			return fmt.Errorf("processes kept changing while being held")
		}

		pids, err := list()
		if err != nil {
			return err
		}

		var seized []int
		for _, pid := range pids {
			if skipped[pid] || skip(pid) {
				skipped[pid] = true
				continue
			}
			tids, err := threads(pid)
			if err != nil {
				continue
			}
			for _, tid := range tids {
				if _, ok := h.tids[tid]; ok {
					continue
				}
				if tracer, _ := tracerOf(pid, tid); tracer != 0 {
					if !skipped[pid] {
						log.Debug().Int("pid", pid).Int("tid", tid).Int("tracer", tracer).Msg("not holding thread traced by another process")
						skipped[pid] = true
						h.skipped++
					}
					continue
				}
				if err := unix.PtraceSeize(tid); err != nil {
					if !errors.Is(err, unix.ESRCH) {
						log.Debug().Err(err).Int("pid", pid).Int("tid", tid).Msg("failed to seize thread, not holding it")
						if !skipped[pid] {
							h.skipped++
						}
						skipped[pid] = true
					}
					continue
				}
				if err := unix.PtraceInterrupt(tid); err != nil {
					log.Trace().Err(err).Int("tid", tid).Msg("failed to interrupt seized thread")
				}
				h.tids[tid] = 0
				seized = append(seized, tid)
			}
		}

		for _, tid := range seized {
			if err := h.waitStopped(tid, deadline); err != nil {
				return err
			}
		}

		if len(seized) == 0 {
			quiet++
		} else {
			quiet = 0
		}
	}

	return nil
}

func (h *Held) waitStopped(tid int, deadline time.Time) error {
	for {
		var ws unix.WaitStatus
		wpid, err := unix.Wait4(tid, &ws, unix.WALL|unix.WNOHANG, nil)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			delete(h.tids, tid)
			return nil
		}
		if wpid == tid {
			switch {
			case ws.Exited() || ws.Signaled():
				delete(h.tids, tid)
			case ws.Stopped():
				if event := (uint32(ws) >> 16) & 0xff; event == 0 {
					h.tids[tid] = ws.StopSignal()
				}
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("thread %d did not stop in time", tid)
		}
		time.Sleep(stopPollInterval)
	}
}

func release(tid int, sig unix.Signal) {
	_, _, errno := unix.Syscall6(unix.SYS_PTRACE, unix.PTRACE_DETACH, uintptr(tid), 0, uintptr(sig), 0, 0)
	if errno == 0 {
		return
	}
	if errno != unix.ESRCH {
		log.Debug().Err(errno).Int("tid", tid).Msg("failed to release held thread")
	}
	var ws unix.WaitStatus
	unix.Wait4(tid, &ws, unix.WALL|unix.WNOHANG, nil)
}

func threads(pid int) ([]int, error) {
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		return nil, err
	}
	tids := make([]int, 0, len(entries))
	for _, e := range entries {
		if tid, err := strconv.Atoi(e.Name()); err == nil {
			tids = append(tids, tid)
		}
	}
	return tids, nil
}

func tracerOf(pid, tid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/status", pid, tid))
	if err != nil {
		return 0, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if v, ok := strings.CutPrefix(scanner.Text(), "TracerPid:"); ok {
			return strconv.Atoi(strings.TrimSpace(v))
		}
	}
	return 0, nil
}

func Session(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	i := bytes.LastIndexByte(data, ')')
	if i < 0 {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(data[i+1:]))
	if len(fields) < 4 {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	return strconv.Atoi(fields[3])
}
