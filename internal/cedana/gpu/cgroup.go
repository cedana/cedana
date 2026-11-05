package gpu

// A GPU controller and its workers run in the cgroup of the application they serve (the
// controller moves itself there before spawning the application's first worker). The cgroup
// freezer would then freeze
// the very workers that do the GPU dump, so a GPU dump freezes the application with ptrace
// instead, which leaves them alone:
//
//   - CRIU seizes the application's process tree with ptrace, as it does without a freezer.
//   - Once it has, we hold the rest of the cgroup (processes outside that tree, like those of a
//     kubectl exec or an exec probe) with ptrace too, as the cgroup freezer would have, and let
//     them go when the dump ends.
//
// A ptrace-stop is invisible to the processes held: no SIGCHLD to their parent, no SIGCONT.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/ptrace"
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
)

const HOLD_TIMEOUT = 30 * time.Second

var CGROUPS_MOUNT = "/sys/fs/cgroup"

func (p *pool) freezeWithPtrace(callback *criu_client.NotifyCallback, id string, log zerolog.Logger) {
	initialize := callback.InitializeDumpFunc
	queryExtFiles := callback.QueryExtFilesFunc
	finalize := callback.FinalizeDumpFunc

	var cgroup string
	var held *ptrace.Held

	callback.InitializeDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		cgroup, held = "", nil

		if controller := p.Get(id); controller != nil && cgroupFrozen(controller.PID) {
			return fmt.Errorf("the GPU controller is frozen, with its container: unfreeze it before dumping")
		}

		if opts.GetFreezeCgroup() != "" {
			cgroup = opts.GetFreezeCgroup()
			opts.FreezeCgroup = nil
			log.Debug().Str("cgroup", cgroup).Msg("freezing with ptrace instead of the cgroup freezer, which would freeze the GPU workers")
		}

		if initialize != nil {
			return initialize(ctx, opts)
		}
		return nil
	}

	callback.QueryExtFilesFunc = func(ctx context.Context) ([]string, error) {
		if cgroup != "" {
			sessions := p.controllerSessions()
			self := os.Getpid()
			skip := func(pid int) bool {
				if pid == self {
					return true
				}
				sid, err := ptrace.Session(pid)
				return err != nil || sessions[sid] || isController(sid)
			}

			var err error
			held, err = ptrace.Hold(func() ([]int, error) { return cgroupProcs(cgroup) }, skip, HOLD_TIMEOUT)
			if err != nil {
				return nil, fmt.Errorf("failed to hold the processes outside the dumped tree in %s: %w", cgroup, err)
			}
			if n := held.Len(); n > 0 || held.Skipped() > 0 {
				log.Debug().Str("cgroup", cgroup).Int("threads", n).Int("skipped", held.Skipped()).Msg("holding the processes outside the dumped tree")
			}
		}

		if queryExtFiles != nil {
			return queryExtFiles(ctx)
		}
		return nil, nil
	}

	callback.FinalizeDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts, criuErr error) error {
		if held != nil {
			held.Release()
		}
		if finalize != nil {
			return finalize(ctx, opts, criuErr)
		}
		return nil
	}
}

func (p *pool) controllerSessions() map[int]bool {
	sessions := map[int]bool{}
	p.Range(func(_, value any) bool {
		if c := value.(*controller); c.PID != 0 {
			sessions[int(c.PID)] = true
		}
		return true
	})
	return sessions
}

func cgroupProcs(root string) ([]int, error) {
	seen := map[int]bool{}
	var pids []int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path != root && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(path, "cgroup.procs"))
		if err != nil {
			if path != root && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		for _, field := range strings.Fields(string(data)) {
			if pid, err := strconv.Atoi(field); err == nil && !seen[pid] {
				seen[pid] = true
				pids = append(pids, pid)
			}
		}
		return nil
	})
	return pids, err
}

func cgroupFrozen(pid uint32) bool {
	if pid == 0 {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return false
	}

	var fsStat unix.Statfs_t
	unified := unix.Statfs(CGROUPS_MOUNT, &fsStat) == nil && fsStat.Type == unix.CGROUP2_SUPER_MAGIC

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		switch {
		case unified && parts[0] == "0" && parts[1] == "":
			events, err := os.ReadFile(filepath.Join(CGROUPS_MOUNT, parts[2], "cgroup.events"))
			return err == nil && strings.Contains(string(events), "frozen 1")
		case !unified && hasController(parts[1], "freezer"):
			state, err := os.ReadFile(filepath.Join(CGROUPS_MOUNT, "freezer", parts[2], "freezer.state"))
			return err == nil && strings.TrimSpace(string(state)) != "THAWED"
		}
	}
	return false
}

func hasController(list, name string) bool {
	return slices.Contains(strings.Split(list, ","), name)
}

func isController(pid int) bool {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	return err == nil && filepath.Base(exe) == CONTROLLER_PROCESS_NAME
}
