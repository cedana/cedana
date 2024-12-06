package runc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/opencontainers/runtime-spec/specs-go"
	selinux "github.com/opencontainers/selinux/go-selinux"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/system/kernelversion"
	"github.com/opencontainers/runc/libcontainer/utils"
)

var errEmptyID = errors.New("container id cannot be empty")

// newProcess returns a new libcontainer Process with the arguments from the
// spec and stdio from the current process.
func newProcess(p specs.Process) (*libcontainer.Process, error) {
	lp := &libcontainer.Process{
		Args: p.Args,
		Env:  p.Env,
		// TODO: fix libcontainer's API to better support uid/gid in a typesafe way.
		User:            fmt.Sprintf("%d:%d", p.User.UID, p.User.GID),
		Cwd:             p.Cwd,
		Label:           p.SelinuxLabel,
		NoNewPrivileges: &p.NoNewPrivileges,
		AppArmorProfile: p.ApparmorProfile,
	}

	if p.ConsoleSize != nil {
		lp.ConsoleWidth = uint16(p.ConsoleSize.Width)
		lp.ConsoleHeight = uint16(p.ConsoleSize.Height)
	}

	if p.Scheduler != nil {
		s := *p.Scheduler
		lp.Scheduler = &s
	}

	if p.IOPriority != nil {
		ioPriority := *p.IOPriority
		lp.IOPriority = &ioPriority
	}

	if p.Capabilities != nil {
		lp.Capabilities = &configs.Capabilities{}
		lp.Capabilities.Bounding = p.Capabilities.Bounding
		lp.Capabilities.Effective = p.Capabilities.Effective
		lp.Capabilities.Inheritable = p.Capabilities.Inheritable
		lp.Capabilities.Permitted = p.Capabilities.Permitted
		lp.Capabilities.Ambient = p.Capabilities.Ambient
	}
	for _, gid := range p.User.AdditionalGids {
		lp.AdditionalGroups = append(lp.AdditionalGroups, strconv.FormatUint(uint64(gid), 10))
	}
	for _, rlimit := range p.Rlimits {
		rl, err := CreateLibContainerRlimit(rlimit)
		if err != nil {
			return nil, err
		}
		lp.Rlimits = append(lp.Rlimits, rl)
	}
	return lp, nil
}

// setupIO modifies the given process config according to the options.
func setupIO(process *libcontainer.Process, rootuid, rootgid int, createTTY, detach bool, sockpath string) (*tty, error) {
	if createTTY {
		process.Stdin = nil
		process.Stdout = nil
		process.Stderr = nil
		t := &tty{}
		if !detach {
			if err := t.initHostConsole(); err != nil {
				return nil, err
			}
			parent, child, err := utils.NewSockPair("console")
			if err != nil {
				return nil, err
			}
			process.ConsoleSocket = child
			t.postStart = append(t.postStart, parent, child)
			t.consoleC = make(chan error, 1)
			go func() {
				t.consoleC <- t.recvtty(parent)
			}()
		} else {
			// the caller of runc will handle receiving the console master
			conn, err := net.Dial("unix", sockpath)
			if err != nil {
				return nil, err
			}
			uc, ok := conn.(*net.UnixConn)
			if !ok {
				return nil, errors.New("casting to UnixConn failed")
			}
			t.postStart = append(t.postStart, uc)
			socket, err := uc.File()
			if err != nil {
				return nil, err
			}
			t.postStart = append(t.postStart, socket)
			process.ConsoleSocket = socket
		}
		return t, nil
	}
	// when runc will detach the caller provides the stdio to runc via runc's 0,1,2
	// and the container's process inherits runc's stdio.
	if detach {
		inheritStdio(process)
		return &tty{}, nil
	}
	return setupProcessPipes(process, rootuid, rootgid, os.Stdin, os.Stdout, os.Stderr)
}

// createPidFile creates a file containing the PID,
// doing so atomically (via create and rename).
func createPidFile(path string, process *libcontainer.Process) error {
	pid, err := process.Pid()
	if err != nil {
		return err
	}
	var (
		tmpDir  = filepath.Dir(path)
		tmpName = filepath.Join(tmpDir, "."+filepath.Base(path))
	)
	f, err := os.OpenFile(tmpName, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0o666)
	if err != nil {
		return err
	}
	_, err = f.WriteString(strconv.Itoa(pid))
	f.Close()
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

type Runner struct {
	Init            bool
	EnableSubreaper bool
	ShouldDestroy   bool
	Detach          bool
	ListenFDs       []*os.File
	PreserveFDs     int
	PidFile         string
	ConsoleSocket   string
	PidfdSocket     string
	Container       *libcontainer.Container
	Action          CtAct
	NotifySocket    *notifySocket
	CriuOpts        *libcontainer.CriuOpts
	SubCgroupPaths  map[string]string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (r *Runner) Run(config *specs.Process) (int, error) {
	var err error
	defer func() {
		if err != nil {
			r.Destroy()
		}
	}()
	if err = r.CheckTerminal(config); err != nil {
		return -1, err
	}
	process, err := newProcess(*config)
	if err != nil {
		return -1, err
	}
	process.LogLevel = strconv.Itoa(int(logrus.GetLevel()))
	// Populate the fields that come from Runner.
	process.Init = r.Init
	process.SubCgroupPaths = r.SubCgroupPaths
	if len(r.ListenFDs) > 0 {
		process.Env = append(process.Env, "LISTEN_FDS="+strconv.Itoa(len(r.ListenFDs)), "LISTEN_PID=1")
		process.ExtraFiles = append(process.ExtraFiles, r.ListenFDs...)
	}
	baseFd := 3 + len(process.ExtraFiles)
	procSelfFd, closer := utils.ProcThreadSelf("fd/")
	defer closer()
	for i := baseFd; i < baseFd+r.PreserveFDs; i++ {
		_, err = os.Stat(filepath.Join(procSelfFd, strconv.Itoa(i)))
		if err != nil {
			return -1, fmt.Errorf("unable to stat preserved-fd %d (of %d): %w", i-baseFd, r.PreserveFDs, err)
		}
		process.ExtraFiles = append(process.ExtraFiles, os.NewFile(uintptr(i), "PreserveFD:"+strconv.Itoa(i)))
	}
	rootuid, err := r.Container.Config().HostRootUID()
	if err != nil {
		return -1, err
	}
	rootgid, err := r.Container.Config().HostRootGID()
	if err != nil {
		return -1, err
	}
	detach := r.Detach || (r.Action == CT_ACT_CREATE)
	// Setting up IO is a two stage process. We need to modify process to deal
	// with detaching containers, and then we get a tty after the container has
	// started.
	handler := newSignalHandler(r.EnableSubreaper, r.NotifySocket)
	tty, err := setupProcessPipes(process, rootuid, rootgid, r.Stdin, r.Stdout, r.Stderr)
	if err != nil {
		return -1, err
	}
	defer tty.Close()

	if r.PidfdSocket != "" {
		connClose, err := setupPidfdSocket(process, r.PidfdSocket)
		if err != nil {
			return -1, err
		}
		defer connClose()
	}

	switch r.Action {
	case CT_ACT_CREATE:
		err = r.Container.Start(process)
	case CT_ACT_RESTORE:
		err = r.Container.Restore(process, r.CriuOpts)
	case CT_ACT_RUN:
		err = r.Container.Run(process)
	default:
		panic("Unknown action")
	}
	if err != nil {
		return -1, err
	}
	if err = tty.waitConsole(); err != nil {
		r.Terminate(process)
		return -1, err
	}
	tty.ClosePostStart()
	if r.PidFile != "" {
		if err = createPidFile(r.PidFile, process); err != nil {
			r.Terminate(process)
			return -1, err
		}
	}
	status, err := handler.forward(process, tty, detach)
	if err != nil {
		r.Terminate(process)
	}
	if detach {
		return 0, nil
	}
	if err == nil {
		r.Destroy()
	}
	return status, err
}

func (r *Runner) Destroy() {
	if r.ShouldDestroy {
		if err := r.Container.Destroy(); err != nil {
			logrus.Warn(err)
		}
	}
}

func (r *Runner) Terminate(p *libcontainer.Process) {
	_ = p.Signal(unix.SIGKILL)
	_, _ = p.Wait()
}

func (r *Runner) CheckTerminal(config *specs.Process) error {
	detach := r.Detach || (r.Action == CT_ACT_CREATE)
	// Check command-line for sanity.
	if detach && config.Terminal && r.ConsoleSocket == "" {
		return errors.New("cannot allocate tty if runc will detach without setting console socket")
	}
	if (!detach || !config.Terminal) && r.ConsoleSocket != "" {
		return errors.New("cannot use console socket if runc will not detach or allocate tty")
	}
	return nil
}

func validateProcessSpec(spec *specs.Process) error {
	if spec == nil {
		return errors.New("process property must not be empty")
	}
	if spec.Cwd == "" {
		return errors.New("Cwd property must not be empty")
	}
	if !filepath.IsAbs(spec.Cwd) {
		return errors.New("Cwd must be an absolute path")
	}
	if len(spec.Args) == 0 {
		return errors.New("args must not be empty")
	}
	if spec.SelinuxLabel != "" && !selinux.GetEnabled() {
		return errors.New("selinux label is specified in config, but selinux is disabled or not supported")
	}
	return nil
}

type CtAct uint8

const (
	CT_ACT_CREATE CtAct = iota + 1
	CT_ACT_RUN
	CT_ACT_RESTORE
)

func setupPidfdSocket(process *libcontainer.Process, sockpath string) (_clean func(), _ error) {
	linux530 := kernelversion.KernelVersion{Kernel: 5, Major: 3}
	ok, err := kernelversion.GreaterEqualThan(linux530)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("--pidfd-socket requires >= v5.3 kernel")
	}

	conn, err := net.Dial("unix", sockpath)
	if err != nil {
		return nil, fmt.Errorf("failed to dail %s: %w", sockpath, err)
	}

	uc, ok := conn.(*net.UnixConn)
	if !ok {
		conn.Close()
		return nil, errors.New("failed to cast to UnixConn")
	}

	socket, err := uc.File()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to dup socket: %w", err)
	}

	process.PidfdSocket = socket
	return func() {
		conn.Close()
	}, nil
}
