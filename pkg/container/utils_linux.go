package container

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/cedana/runc/libcontainer"
	"github.com/cedana/runc/libcontainer/cgroups"
	"github.com/cedana/runc/libcontainer/cgroups/manager"
	"github.com/cedana/runc/libcontainer/configs"
	"github.com/cedana/runc/libcontainer/configs/validate"
	"github.com/cedana/runc/libcontainer/specconv"
	"github.com/cedana/runc/libcontainer/system"
	"github.com/cedana/runc/libcontainer/utils"
	criurpc "github.com/checkpoint-restore/go-criu/v6/rpc"
	"github.com/coreos/go-systemd/v22/activation"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/moby/sys/user"
	"github.com/opencontainers/runtime-spec/specs-go"
	selinux "github.com/opencontainers/selinux/go-selinux"
	"github.com/rs/zerolog/log"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

var errEmptyID = errors.New("container id cannot be empty")

// newProcess returns a new libcontainer Process with the arguments from the
// spec and stdio from the current process.
func newProcess(p specs.Process) (*Process, error) {
	lp := &Process{
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
		rl, err := createLibContainerRlimit(rlimit)
		if err != nil {
			return nil, err
		}
		lp.Rlimits = append(lp.Rlimits, rl)
	}
	return lp, nil
}

func destroy(container *libcontainer.Container) {
	if err := container.Destroy(); err != nil {
		log.Err(err)
	}
}

// setupIO modifies the given process config according to the options.
func setupIO(process *Process, rootuid, rootgid int, createTTY, detach bool, sockpath string) (*tty, error) {
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
	return setupProcessPipes(process, rootuid, rootgid)
}

// createPidFile creates a file with the processes pid inside it atomically
// it creates a temp file with the paths filename + '.' infront of it
// then renames the file
func createPidFile(path string, process *Process) error {
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

func CreateContainer(context *RuncOpts, id string, spec *specs.Spec) (*RuncContainer, error) {
	rootlessCg, err := shouldUseRootlessCgroupManager(context)
	if err != nil {
		return nil, err
	}
	config, err := specconv.CreateLibcontainerConfig(&specconv.CreateOpts{
		CgroupName:       id,
		UseSystemdCgroup: context.SystemdCgroup,
		NoPivotRoot:      context.NoPivot,
		NoNewKeyring:     context.NoNewKeyring,
		Spec:             spec,
		RootlessEUID:     os.Geteuid() != 0,
		RootlessCgroups:  rootlessCg,
		NoMountFallback:  context.NoMountFallback,
	})
	if err != nil {
		return nil, err
	}

	root := context.Root
	return Create(root, id, config)
}

var (
	ErrExist      = errors.New("container with given ID already exists")
	ErrInvalidID  = errors.New("invalid container ID format")
	ErrNotExist   = errors.New("container does not exist")
	ErrPaused     = errors.New("container paused")
	ErrRunning    = errors.New("container still running")
	ErrNotRunning = errors.New("container not running")
	ErrNotPaused  = errors.New("container not paused")
)

func validateID(id string) error {
	if len(id) < 1 {
		return ErrInvalidID
	}

	// Allowed characters: 0-9 A-Z a-z _ + - .
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_':
		case c == '+':
		case c == '-':
		case c == '.':
		default:
			return ErrInvalidID
		}

	}

	if string(os.PathSeparator)+id != utils.CleanPath(string(os.PathSeparator)+id) {
		return ErrInvalidID
	}

	return nil
}

func Create(root, id string, config *configs.Config) (*RuncContainer, error) {
	if root == "" {
		return nil, errors.New("root not set")
	}
	if err := validateID(id); err != nil {
		return nil, err
	}
	if err := validate.Validate(config); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	stateDir, err := securejoin.SecureJoin(root, id)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(stateDir); err == nil {
		return nil, ErrExist
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	cm, err := manager.New(config.Cgroups)
	if err != nil {
		return nil, err
	}

	// Check that cgroup does not exist or empty (no processes).
	// Note for cgroup v1 this check is not thorough, as there are multiple
	// separate hierarchies, while both Exists() and GetAllPids() only use
	// one for "devices" controller (assuming others are the same, which is
	// probably true in almost all scenarios). Checking all the hierarchies
	// would be too expensive.
	if cm.Exists() {
		// pids, err := cm.GetAllPids()
		// Reading PIDs can race with cgroups removal, so ignore ENOENT and ENODEV.
		if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, unix.ENODEV) {
			return nil, fmt.Errorf("unable to get cgroup PIDs: %w", err)
		}
		//if len(pids) != 0 {
		//return nil, fmt.Errorf("container's cgroup is not empty: %d process(es) found", len(pids))
		//}
	}

	// Check that cgroup is not frozen. Do not use Exists() here
	// since in cgroup v1 it only checks "devices" controller.
	st, err := cm.GetFreezerState()
	if err != nil {
		return nil, fmt.Errorf("unable to get cgroup freezer state: %w", err)
	}
	if st == configs.Frozen {
		return nil, errors.New("container's cgroup unexpectedly frozen")
	}

	// Parent directory is already created above, so Mkdir is enough.
	if err := os.Mkdir(stateDir, 0o711); err != nil {
		return nil, err
	}
	c := &RuncContainer{
		Id:              id,
		StateDir:        stateDir,
		Config:          config,
		CgroupManager:   cm,
		IntelRdtManager: NewManager(config, id, ""),
	}
	c.State = &stoppedState{c: c}
	return c, nil
}

type stoppedState struct {
	c *RuncContainer
}

func (b *stoppedState) status() Status {
	return Stopped
}

const (
	// Created is the status that denotes the container exists but has not been run yet.
	Created Status = iota
	// Running is the status that denotes the container exists and is running.
	Running
	// Paused is the status that denotes the container exists, but all its processes are paused.
	Paused
	// Stopped is the status that denotes the container does not have a created or running process.
	Stopped
)

func (b *stoppedState) transition(s containerState) error {
	switch s.(type) {
	case *runningState, *restoredState:
		b.c.State = s
		return nil
	case *stoppedState:
		return nil
	}
	return fmt.Errorf("transition err")
}

type restoredState struct {
	imageDir string
	c        *RuncContainer
}

func (r *restoredState) status() Status {
	return Running
}

func (r *restoredState) transition(s containerState) error {
	switch s.(type) {
	case *stoppedState, *runningState:
		return nil
	}
	return fmt.Errorf("transition err")
}

func destroyContainer(c *RuncContainer) error {
	// Usually, when a container init is gone, all other processes in its
	// cgroup are killed by the kernel. This is not the case for a shared
	// PID namespace container, which may have some processes left after
	// its init is killed or exited.
	//
	// As the container without init process running is considered stopped,
	// and destroy is supposed to remove all the container resources, we need
	// to kill those processes here.
	if !c.Config.Namespaces.IsPrivate(configs.NEWPID) {
		_ = signalAllProcesses(c.CgroupManager, unix.SIGKILL)
	}
	if err := c.CgroupManager.Destroy(); err != nil {
		return fmt.Errorf("unable to remove container's cgroup: %w", err)
	}
	if c.IntelRdtManager != nil {
		if err := c.IntelRdtManager.Destroy(); err != nil {
			return fmt.Errorf("unable to remove container's IntelRDT group: %w", err)
		}
	}
	if err := os.RemoveAll(c.StateDir); err != nil {
		return fmt.Errorf("unable to remove container state dir: %w", err)
	}
	c.InitProcess = nil
	err := runPoststopHooks(c)
	c.State = &stoppedState{c: c}
	return err
}

func runPoststopHooks(c *RuncContainer) error {
	hooks := c.Config.Hooks
	if hooks == nil {
		return nil
	}

	s, err := c.currentOCIState()
	if err != nil {
		return err
	}
	s.Status = specs.StateStopped

	return hooks.Run(configs.Poststop, s)
}

// ID returns the container's unique ID
func (c *RuncContainer) ID() string {
	return c.Id
}

const (
	stateFilename    = "state.json"
	execFifoFilename = "exec.fifo"
)

func (c *RuncContainer) isPaused() (bool, error) {
	state, err := c.CgroupManager.GetFreezerState()
	if err != nil {
		return false, err
	}
	return state == configs.Frozen, nil
}

// refreshState needs to be called to verify that the current state on the
// container is what is true.  Because consumers of libcontainer can use it
// out of process we need to verify the container's status based on runtime
// information and not rely on our in process info.
func (c *RuncContainer) refreshState() error {
	paused, err := c.isPaused()
	if err != nil {
		return err
	}
	if paused {
		return c.State.transition(&pausedState{c: c})
	}
	if !c.hasInit() {
		return c.State.transition(&stoppedState{c: c})
	}
	// The presence of exec fifo helps to distinguish between
	// the created and the running states.
	if _, err := os.Stat(filepath.Join(c.StateDir, execFifoFilename)); err == nil {
		return c.State.transition(&createdState{c: c})
	}
	return c.State.transition(&runningState{c: c})
}

func (c *RuncContainer) currentStatus() (Status, error) {
	if err := c.refreshState(); err != nil {
		return -1, err
	}
	return c.State.status(), nil
}

func (s Status) String() string {
	switch s {
	case Created:
		return "created"
	case Running:
		return "running"
	case Paused:
		return "paused"
	case Stopped:
		return "stopped"
	default:
		return "unknown"
	}
}

func (c *RuncContainer) currentOCIState() (*specs.State, error) {
	bundle, annotations := utils.Annotations(c.Config.Labels)
	state := &specs.State{
		Version:     specs.Version,
		ID:          c.ID(),
		Bundle:      bundle,
		Annotations: annotations,
	}
	status, err := c.currentStatus()
	if err != nil {
		return nil, err
	}
	state.Status = specs.ContainerState(status.String())
	if status != Stopped {
		if c.InitProcess != nil {
			state.Pid = c.InitProcess.pid()
		}
	}
	return state, nil
}

// signalAllProcesses freezes then iterates over all the processes inside the
// manager's cgroups sending the signal s to them.
func signalAllProcesses(m cgroups.Manager, s unix.Signal) error {
	if !m.Exists() {
		return ErrNotRunning
	}
	// Use cgroup.kill, if available.
	if s == unix.SIGKILL {
		if p := m.Path(""); p != "" { // Either cgroup v2 or hybrid.
			err := cgroups.WriteFile(p, "cgroup.kill", "1")
			if err == nil || !errors.Is(err, os.ErrNotExist) {
				return err
			}
			// Fallback to old implementation.
		}
	}

	if err := m.Freeze(configs.Frozen); err != nil {
		logrus.Warn(err)
	}
	pids, err := m.GetAllPids()
	if err != nil {
		if err := m.Freeze(configs.Thawed); err != nil {
			logrus.Warn(err)
		}
		return err
	}
	for _, pid := range pids {
		err := unix.Kill(pid, s)
		if err != nil && err != unix.ESRCH {
			logrus.Warnf("kill %d: %v", pid, err)
		}
	}
	if err := m.Freeze(configs.Thawed); err != nil {
		logrus.Warn(err)
	}

	return nil
}

func (r *restoredState) destroy() error {
	if _, err := os.Stat(filepath.Join(r.c.StateDir, "checkpoint")); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	}
	return destroyContainer(r.c)
}

type createdState struct {
	c *RuncContainer
}

func (i *createdState) status() Status {
	return Created
}

func (i *createdState) transition(s containerState) error {
	switch s.(type) {
	case *runningState, *pausedState, *stoppedState:
		i.c.State = s
		return nil
	case *createdState:
		return nil
	}
	return fmt.Errorf("transition err")
}

func (i *createdState) destroy() error {
	_ = i.c.InitProcess.signal(unix.SIGKILL)
	return destroyContainer(i.c)
}

type pausedState struct {
	c *RuncContainer
}

func (p *pausedState) status() Status {
	return Paused
}

func (p *pausedState) transition(s containerState) error {
	switch s.(type) {
	case *runningState, *stoppedState:
		p.c.State = s
		return nil
	case *pausedState:
		return nil
	}
	return fmt.Errorf("transition err")
}

// hasInit tells whether the container init process exists.
func (c *RuncContainer) hasInit() bool {
	if c.InitProcess == nil {
		return false
	}
	pid := c.InitProcess.pid()
	stat, err := system.Stat(pid)
	if err != nil {
		return false
	}
	if stat.StartTime != c.InitProcessStartTime || stat.State == system.Zombie || stat.State == system.Dead {
		return false
	}
	return true
}

func (p *pausedState) destroy() error {
	if p.c.hasInit() {
		return ErrPaused
	}
	if err := p.c.CgroupManager.Freeze(configs.Thawed); err != nil {
		return err
	}
	return destroyContainer(p.c)
}

type runningState struct {
	c *RuncContainer
}

func (r *runningState) status() Status {
	return Running
}

func (r *runningState) transition(s containerState) error {
	switch s.(type) {
	case *stoppedState:
		if r.c.hasInit() {
			return ErrRunning
		}
		r.c.State = s
		return nil
	case *pausedState:
		r.c.State = s
		return nil
	case *runningState:
		return nil
	}
	return fmt.Errorf("transition err")
}

func (r *runningState) destroy() error {
	if r.c.hasInit() {
		return ErrRunning
	}
	return destroyContainer(r.c)
}

func (b *stoppedState) destroy() error {
	return destroyContainer(b.c)
}

type processOperations interface {
	wait() (*os.ProcessState, error)
	signal(sig os.Signal) error
	pid() int
}

var errInvalidProcess = errors.New("invalid process")

// Pid returns the process ID
func (p Process) Pid() (int, error) {
	// math.MinInt32 is returned here, because it's invalid value
	// for the kill() system call.
	if p.ops == nil {
		return math.MinInt32, errInvalidProcess
	}
	return p.ops.pid(), nil
}

type Process struct {
	// The command to be run followed by any arguments.
	Args []string

	// Env specifies the environment variables for the process.
	Env []string

	// User will set the uid and gid of the executing process running inside the container
	// local to the container's user and group configuration.
	User string

	// AdditionalGroups specifies the gids that should be added to supplementary groups
	// in addition to those that the user belongs to.
	AdditionalGroups []string

	// Cwd will change the processes current working directory inside the container's rootfs.
	Cwd string

	// Stdin is a pointer to a reader which provides the standard input stream.
	Stdin io.Reader

	// Stdout is a pointer to a writer which receives the standard output stream.
	Stdout io.Writer

	// Stderr is a pointer to a writer which receives the standard error stream.
	Stderr io.Writer

	// ExtraFiles specifies additional open files to be inherited by the container
	ExtraFiles []*os.File

	// Initial sizings for the console
	ConsoleWidth  uint16
	ConsoleHeight uint16

	// Capabilities specify the capabilities to keep when executing the process inside the container
	// All capabilities not specified will be dropped from the processes capability mask
	Capabilities *configs.Capabilities

	// AppArmorProfile specifies the profile to apply to the process and is
	// changed at the time the process is execed
	AppArmorProfile string

	// Label specifies the label to apply to the process.  It is commonly used by selinux
	Label string

	// NoNewPrivileges controls whether processes can gain additional privileges.
	NoNewPrivileges *bool

	// Rlimits specifies the resource limits, such as max open files, to set in the container
	// If Rlimits are not set, the container will inherit rlimits from the parent process
	Rlimits []configs.Rlimit

	// ConsoleSocket provides the masterfd console.
	ConsoleSocket *os.File

	// Init specifies whether the process is the first process in the container.
	Init bool

	ops processOperations

	// LogLevel is a string containing a numeric representation of the current
	// log level (i.e. "4", but never "info"). It is passed on to runc init as
	// _LIBCONTAINER_LOGLEVEL environment variable.
	LogLevel string

	// SubCgroupPaths specifies sub-cgroups to run the process in.
	// Map keys are controller names, map values are paths (relative to
	// container's top-level cgroup).
	//
	// If empty, the default top-level container's cgroup is used.
	//
	// For cgroup v2, the only key allowed is "".
	SubCgroupPaths map[string]string
}

// Wait waits for the process to exit.
// Wait releases any resources associated with the Process
func (p Process) Wait() (*os.ProcessState, error) {
	if p.ops == nil {
		return nil, fmt.Errorf("Error waiting for process")
	}
	return p.ops.wait()
}

// Signal sends a signal to the Process.
func (p Process) Signal(sig os.Signal) error {
	if p.ops == nil {
		return fmt.Errorf("Error sending signal to process")
	}
	return p.ops.signal(sig)
}

type IO struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser
}

// InitializeIO creates pipes for use with the process's stdio and returns the
// opposite side for each. Do not use this if you want to have a pseudoterminal
// set up for you by libcontainer (TODO: fix that too).
// TODO: This is mostly unnecessary, and should be handled by clients.
func (p *Process) InitializeIO(rootuid, rootgid int) (i *IO, err error) {
	var fds []uintptr
	i = &IO{}
	// cleanup in case of an error
	defer func() {
		if err != nil {
			for _, fd := range fds {
				_ = unix.Close(int(fd))
			}
		}
	}()
	// STDIN
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	fds = append(fds, r.Fd(), w.Fd())
	p.Stdin, i.Stdin = r, w
	// STDOUT
	if r, w, err = os.Pipe(); err != nil {
		return nil, err
	}
	fds = append(fds, r.Fd(), w.Fd())
	p.Stdout, i.Stdout = w, r
	// STDERR
	if r, w, err = os.Pipe(); err != nil {
		return nil, err
	}
	fds = append(fds, r.Fd(), w.Fd())
	p.Stderr, i.Stderr = w, r
	// change ownership of the pipes in case we are in a user namespace
	for _, fd := range fds {
		if err := unix.Fchown(int(fd), rootuid, rootgid); err != nil {
			return nil, &os.PathError{Op: "fchown", Path: "fd " + strconv.Itoa(int(fd)), Err: err}
		}
	}
	return i, nil
}

func mountViaFDs(source string, srcFD *int, target, dstFD, fstype string, flags uintptr, data string) error {
	src := source
	if srcFD != nil {
		src = "/proc/self/fd/" + strconv.Itoa(*srcFD)
	}
	dst := target
	if dstFD != "" {
		dst = dstFD
	}
	if err := unix.Mount(src, dst, fstype, flags, data); err != nil {
		return err
	}
	return nil
}

func runcMount(source, target, fstype string, flags uintptr, data string) error {
	return mountViaFDs(source, nil, target, "", fstype, flags, data)
}

func (c *RuncContainer) criuSupportsExtNS(t configs.NamespaceType) bool {
	var minVersion int
	switch t {
	case configs.NEWNET:
		// CRIU supports different external namespace with different released CRIU versions.
		// For network namespaces to work we need at least criu 3.11.0 => 31100.
		minVersion = 31100
	case configs.NEWPID:
		// For PID namespaces criu 31500 is needed.
		minVersion = 31500
	default:
		return false
	}
	return c.checkCriuVersion(minVersion) == nil
}

func criuNsToKey(t configs.NamespaceType) string {
	return "extRoot" + strings.Title(configs.NsName(t)) + "NS" //nolint:staticcheck // SA1019: strings.Title is deprecated
}

func (c *RuncContainer) handleRestoringExternalNamespaces(rpcOpts *criurpc.CriuOpts, extraFiles *[]*os.File, t configs.NamespaceType) error {
	if !c.criuSupportsExtNS(t) {
		return nil
	}

	nsPath := c.Config.Namespaces.PathOf(t)
	if nsPath == "" {
		return nil
	}
	// CRIU wants the information about an existing namespace
	// like this: --inherit-fd fd[<fd>]:<key>
	// The <key> needs to be the same as during checkpointing.
	// We are always using 'extRoot<TYPE>NS' as the key in this.
	nsFd, err := os.Open(nsPath)
	if err != nil {
		logrus.Errorf("If a specific network namespace is defined it must exist: %s", err)
		return fmt.Errorf("Requested network namespace %v does not exist", nsPath)
	}
	inheritFd := &criurpc.InheritFd{
		Key: proto.String(criuNsToKey(t)),
		// The offset of four is necessary because 0, 1, 2 and 3 are
		// already used by stdin, stdout, stderr, 'criu swrk' socket.
		Fd: proto.Int32(int32(4 + len(*extraFiles))),
	}
	rpcOpts.InheritFd = append(rpcOpts.InheritFd, inheritFd)
	// All open FDs need to be transferred to CRIU via extraFiles
	*extraFiles = append(*extraFiles, nsFd)

	return nil
}

func (c *RuncContainer) handleRestoringExternalPidNamespace(rpcOpts *criurpc.CriuOpts, extraFiles *[]*os.File, initPid string) error {
	nsPath := fmt.Sprintf("/proc/%s/ns/pid", initPid)

	// CRIU wants the information about an existing namespace
	// like this: --inherit-fd fd[<fd>]:<key>
	// The <key> needs to be the same as during checkpointing.
	// We are always using 'extRoot<TYPE>NS' as the key in this.

	nsFd, err := os.Open(nsPath)
	if err != nil {
		logrus.Errorf("If a specific pid namespace is defined it must exist: %s", err)
		return fmt.Errorf("Requested network namespace %v does not exist", nsPath)
	}
	inheritFd := &criurpc.InheritFd{
		Key: proto.String("extRootPidNS"),
		// The offset of four is necessary because 0, 1, 2 and 3 are
		// already used by stdin, stdout, stderr, 'criu swrk' socket.
		Fd: proto.Int32(int32(4 + len(*extraFiles))),
	}
	rpcOpts.InheritFd = append(rpcOpts.InheritFd, inheritFd)
	// All open FDs need to be transferred to CRIU via extraFiles
	*extraFiles = append(*extraFiles, nsFd)

	return nil
}

func (c *RuncContainer) handleRestoringNamespaces(rpcOpts *criurpc.CriuOpts, extraFiles *[]*os.File, initPid string) error {
	if initPid != "" {
		if err := c.handleRestoringExternalPidNamespace(rpcOpts, extraFiles, initPid); err != nil {
			return err
		}
	}

	for _, ns := range c.Config.Namespaces {
		switch ns.Type {
		case configs.NEWNET, configs.NEWPID:
			// If the container is running in a network or PID namespace and has
			// a path to the network or PID namespace configured, we will dump
			// that network or PID namespace as an external namespace and we
			// will expect that the namespace exists during restore.
			// This basically means that CRIU will ignore the namespace
			// and expect it to be setup correctly.
			if err := c.handleRestoringExternalNamespaces(rpcOpts, extraFiles, ns.Type); err != nil {
				return err
			}
		default:
			// For all other namespaces except NET and PID CRIU has
			// a simpler way of joining the existing namespace if set
			nsPath := c.Config.Namespaces.PathOf(ns.Type)
			if nsPath == "" {
				continue
			}
			if ns.Type == configs.NEWCGROUP {
				// CRIU has no code to handle NEWCGROUP
				return fmt.Errorf("Do not know how to handle namespace %v", ns.Type)
			}
			// CRIU has code to handle NEWTIME, but it does not seem to be defined in runc

			// CRIU will issue a warning for NEWUSER:
			// criu/namespaces.c: 'join-ns with user-namespace is not fully tested and dangerous'
			rpcOpts.JoinNs = append(rpcOpts.JoinNs, &criurpc.JoinNamespace{
				Ns:     proto.String(configs.NsName(ns.Type)),
				NsFile: proto.String(nsPath),
			})
		}
	}

	return nil
}

func (c *RuncContainer) handleCriuConfigurationFile(rpcOpts *criurpc.CriuOpts) {
	// CRIU will evaluate a configuration starting with release 3.11.
	// Settings in the configuration file will overwrite RPC settings.
	// Look for annotations. The annotation 'org.criu.config'
	// specifies if CRIU should use a different, container specific
	// configuration file.
	configFile, exists := utils.SearchLabels(c.Config.Labels, "org.criu.config")
	if exists {
		// If the annotation 'org.criu.config' exists and is set
		// to a non-empty string, tell CRIU to use that as a
		// configuration file. If the file does not exist, CRIU
		// will just ignore it.
		if configFile != "" {
			rpcOpts.ConfigFile = proto.String(configFile)
		}
		// If 'org.criu.config' exists and is set to an empty
		// string, a runc specific CRIU configuration file will
		// be not set at all.
	} else {
		// If the mentioned annotation has not been found, specify
		// a default CRIU configuration file.
		rpcOpts.ConfigFile = proto.String("/etc/criu/runc.conf")
	}
}

func isPathInPrefixList(path string, prefix []string) bool {
	for _, p := range prefix {
		if strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

type mountEntry struct {
	*configs.Mount
	srcFD *int
}

func (m *mountEntry) src() string {
	if m.srcFD != nil {
		return "/proc/self/fd/" + strconv.Itoa(*m.srcFD)
	}
	return m.Source
}

func isProc(path string) (bool, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return false, &os.PathError{Op: "statfs", Path: path, Err: err}
	}
	return s.Type == unix.PROC_SUPER_MAGIC, nil
}

func checkProcMount(rootfs, dest, source string) error {
	const procPath = "/proc"
	path, err := filepath.Rel(filepath.Join(rootfs, procPath), dest)
	if err != nil {
		return err
	}
	// pass if the mount path is located outside of /proc
	if strings.HasPrefix(path, "..") {
		return nil
	}
	if path == "." {
		// an empty source is pasted on restore
		if source == "" {
			return nil
		}
		// only allow a mount on-top of proc if it's source is "proc"
		isproc, err := isProc(source)
		if err != nil {
			return err
		}
		// pass if the mount is happening on top of /proc and the source of
		// the mount is a proc filesystem
		if isproc {
			return nil
		}
		return fmt.Errorf("%q cannot be mounted because it is not of type proc", dest)
	}

	// Here dest is definitely under /proc. Do not allow those,
	// except for a few specific entries emulated by lxcfs.
	validProcMounts := []string{
		"/proc/cpuinfo",
		"/proc/diskstats",
		"/proc/meminfo",
		"/proc/stat",
		"/proc/swaps",
		"/proc/uptime",
		"/proc/loadavg",
		"/proc/slabinfo",
		"/proc/net/dev",
		"/proc/sys/kernel/ns_last_pid",
	}
	for _, valid := range validProcMounts {
		path, err := filepath.Rel(filepath.Join(rootfs, valid), dest)
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
	}

	return fmt.Errorf("%q cannot be mounted because it is inside /proc", dest)
}

func createIfNotExists(path string, isDir bool) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			if isDir {
				return os.MkdirAll(path, 0o755)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_CREATE, 0o755)
			if err != nil {
				return err
			}
			_ = f.Close()
		}
	}
	return nil
}

func prepareBindMount(m mountEntry, rootfs string) error {
	source := m.src()
	stat, err := os.Stat(source)
	if err != nil {
		// error out if the source of a bind mount does not exist as we will be
		// unable to bind anything to it.
		return err
	}
	// ensure that the destination of the bind mount is resolved of symlinks at mount time because
	// any previous mounts can invalidate the next mount's destination.
	// this can happen when a user specifies mounts within other mounts to cause breakouts or other
	// evil stuff to try to escape the container's rootfs.
	var dest string
	if dest, err = securejoin.SecureJoin(rootfs, m.Destination); err != nil {
		return err
	}
	if err := checkProcMount(rootfs, dest, source); err != nil {
		return err
	}
	if err := createIfNotExists(dest, stat.IsDir()); err != nil {
		return err
	}

	return nil
}

func (c *RuncContainer) makeCriuRestoreMountpoints(m *configs.Mount) error {
	switch m.Device {
	case "cgroup":
		// No mount point(s) need to be created:
		//
		// * for v1, mount points are saved by CRIU because
		//   /sys/fs/cgroup is a tmpfs mount
		//
		// * for v2, /sys/fs/cgroup is a real mount, but
		//   the mountpoint appears as soon as /sys is mounted
		return nil
	case "bind":
		// The prepareBindMount() function checks if source
		// exists. So it cannot be used for other filesystem types.
		// TODO: pass srcFD? Not sure if criu is impacted by issue #2484.
		if err := prepareBindMount(mountEntry{Mount: m}, c.Config.Rootfs); err != nil {
			return err
		}
	default:
		// for all other filesystems just create the mountpoints
		dest, err := securejoin.SecureJoin(c.Config.Rootfs, m.Destination)
		if err != nil {
			return err
		}
		if err := checkProcMount(c.Config.Rootfs, dest, ""); err != nil {
			return err
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// prepareCriuRestoreMounts tries to set up the rootfs of the
// container to be restored in the same way runc does it for
// initial container creation. Even for a read-only rootfs container
// runc modifies the rootfs to add mountpoints which do not exist.
// This function also creates missing mountpoints as long as they
// are not on top of a tmpfs, as CRIU will restore tmpfs content anyway.
func (c *RuncContainer) prepareCriuRestoreMounts(mounts []*configs.Mount) error {
	// First get a list of a all tmpfs mounts
	tmpfs := []string{}
	for _, m := range mounts {
		switch m.Device {
		case "tmpfs":
			tmpfs = append(tmpfs, m.Destination)
		}
	}
	// Now go through all mounts and create the mountpoints
	// if the mountpoints are not on a tmpfs, as CRIU will
	// restore the complete tmpfs content from its checkpoint.
	umounts := []string{}
	defer func() {
		for _, u := range umounts {
			_ = utils.WithProcfd(c.Config.Rootfs, u, func(procfd string) error {
				if e := unix.Unmount(procfd, unix.MNT_DETACH); e != nil {
					if e != unix.EINVAL {
						// Ignore EINVAL as it means 'target is not a mount point.'
						// It probably has already been unmounted.
						logrus.Warnf("Error during cleanup unmounting of %s (%s): %v", procfd, u, e)
					}
				}
				return nil
			})
		}
	}()
	for _, m := range mounts {
		if !isPathInPrefixList(m.Destination, tmpfs) {
			if err := c.makeCriuRestoreMountpoints(m); err != nil {
				return err
			}
			// If the mount point is a bind mount, we need to mount
			// it now so that runc can create the necessary mount
			// points for mounts in bind mounts.
			// This also happens during initial container creation.
			// Without this CRIU restore will fail
			// See: https://github.com/opencontainers/runc/issues/2748
			// It is also not necessary to order the mount points
			// because during initial container creation mounts are
			// set up in the order they are configured.
			if m.Device == "bind" {
				if err := utils.WithProcfd(c.Config.Rootfs, m.Destination, func(dstFD string) error {
					return mountViaFDs(m.Source, nil, m.Destination, dstFD, "", unix.MS_BIND|unix.MS_REC, "")
				}); err != nil {
					return err
				}
				umounts = append(umounts, m.Destination)
			}
		}
	}
	return nil
}

func (c *RuncContainer) restoreNetwork(req *criurpc.CriuReq, criuOpts *CriuOpts) {
	for _, iface := range c.Config.Networks {
		switch iface.Type {
		case "veth":
			veth := new(criurpc.CriuVethPair)
			veth.IfOut = proto.String(iface.HostInterfaceName)
			veth.IfIn = proto.String(iface.Name)
			req.Opts.Veths = append(req.Opts.Veths, veth)
		case "loopback":
			// Do nothing
		}
	}
	for _, i := range criuOpts.VethPairs {
		veth := new(criurpc.CriuVethPair)
		veth.IfOut = proto.String(i.HostInterfaceName)
		veth.IfIn = proto.String(i.ContainerInterfaceName)
		req.Opts.Veths = append(req.Opts.Veths, veth)
	}
}

func (c *RuncContainer) addCriuRestoreMount(req *criurpc.CriuReq, m *configs.Mount) {
	mountDest := strings.TrimPrefix(m.Destination, c.Config.Rootfs)
	if dest, err := securejoin.SecureJoin(c.Config.Rootfs, mountDest); err == nil {
		mountDest = dest[len(c.Config.Rootfs):]
	}
	extMnt := &criurpc.ExtMountMap{
		Key: proto.String(mountDest),
		Val: proto.String(m.Source),
	}
	req.Opts.ExtMnt = append(req.Opts.ExtMnt, extMnt)
}

func getCgroupMounts(m *configs.Mount) ([]*configs.Mount, error) {
	mounts, err := cgroups.GetCgroupMounts(false)
	if err != nil {
		return nil, err
	}

	cgroupPaths, err := cgroups.ParseCgroupFile("/proc/self/cgroup")
	if err != nil {
		return nil, err
	}

	var binds []*configs.Mount

	for _, mm := range mounts {
		dir, err := mm.GetOwnCgroup(cgroupPaths)
		if err != nil {
			return nil, err
		}
		relDir, err := filepath.Rel(mm.Root, dir)
		if err != nil {
			return nil, err
		}
		binds = append(binds, &configs.Mount{
			Device:           "bind",
			Source:           filepath.Join(mm.Mountpoint, relDir),
			Destination:      filepath.Join(m.Destination, filepath.Base(mm.Mountpoint)),
			Flags:            unix.MS_BIND | unix.MS_REC | m.Flags,
			PropagationFlags: m.PropagationFlags,
		})
	}

	return binds, nil
}

func logCriuErrors(dir, file string) {
	lookFor := []byte("Error") // Print the line that contains this...
	const max = 5 + 1          // ... and a few preceding lines.

	logFile := filepath.Join(dir, file)
	f, err := os.Open(logFile)
	if err != nil {
		logrus.Warn(err)
		return
	}
	defer f.Close()

	var lines [max][]byte
	var idx, lineNo, printedLineNo int
	s := bufio.NewScanner(f)
	for s.Scan() {
		lineNo++
		lines[idx] = s.Bytes()
		idx = (idx + 1) % max
		if !bytes.Contains(s.Bytes(), lookFor) {
			continue
		}
		// Found an error.
		if printedLineNo == 0 {
			logrus.Warnf("--- Quoting %q", logFile)
		} else if lineNo-max > printedLineNo {
			// Mark the gap.
			logrus.Warn("...")
		}
		// Print the last lines.
		for add := 0; add < max; add++ {
			i := (idx + add) % max
			s := lines[i]
			actLineNo := lineNo + add - max + 1
			if len(s) > 0 && actLineNo > printedLineNo {
				logrus.Warnf("%d:%s", actLineNo, s)
				printedLineNo = actLineNo
			}
		}
	}
	if printedLineNo != 0 {
		logrus.Warn("---") // End of "Quoting ...".
	}
	if err := s.Err(); err != nil {
		logrus.Warnf("read %q: %v", logFile, err)
	}
}

func (c *RuncContainer) Restore(process *Process, criuOpts *CriuOpts, runcRoot string, bundle string, netPid int) error {
	const logFile = "restore.log"

	crioPidFilePath := filepath.Join(bundle, "pidfile")
	containerdPidFilePath := filepath.Join(bundle, "init.pid")

	c.M.Lock()
	defer c.M.Unlock()

	var extraFiles []*os.File

	// Restore is unlikely to work if os.Geteuid() != 0 || system.RunningInUserNS().
	// (CLI prints a warning)
	// TODO(avagin): Figure out how to make this work nicely. CRIU doesn't have
	//               support for unprivileged restore at the moment.

	// We are relying on the CRIU version RPC which was introduced with CRIU 3.0.0
	if err := c.checkCriuVersion(30000); err != nil {
		return err
	}
	if criuOpts.ImagesDirectory == "" {
		return errors.New("invalid directory to restore checkpoint")
	}
	logDir := criuOpts.ImagesDirectory
	imageDir, err := os.Open(criuOpts.ImagesDirectory)
	if err != nil {
		return err
	}
	defer imageDir.Close()
	// CRIU has a few requirements for a root directory:
	// * it must be a mount point
	// * its parent must not be overmounted
	// c.config.Rootfs is bind-mounted to a temporary directory
	// to satisfy these requirements.
	root := filepath.Join(c.StateDir, "criu-root")
	if err := os.Mkdir(root, 0o755); err != nil {
		return err
	}
	defer os.Remove(root)
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	err = runcMount(c.Config.Rootfs, root, "", unix.MS_BIND|unix.MS_REC, "")
	if err != nil {
		return err
	}
	defer unix.Unmount(root, unix.MNT_DETACH) //nolint: errcheck
	t := criurpc.CriuReqType_RESTORE
	req := &criurpc.CriuReq{
		Type: &t,
		Opts: &criurpc.CriuOpts{
			ImagesDirFd:       proto.Int32(int32(imageDir.Fd())),
			EvasiveDevices:    proto.Bool(true),
			LogLevel:          proto.Int32(4),
			LogFile:           proto.String(logFile),
			RstSibling:        proto.Bool(true),
			Root:              proto.String(root),
			ManageCgroups:     proto.Bool(false),
			NotifyScripts:     proto.Bool(true),
			ShellJob:          proto.Bool(criuOpts.ShellJob),
			ExtUnixSk:         proto.Bool(criuOpts.ExternalUnixConnections),
			TcpEstablished:    proto.Bool(criuOpts.TcpEstablished),
			FileLocks:         proto.Bool(criuOpts.FileLocks),
			EmptyNs:           proto.Uint32(criuOpts.EmptyNs),
			OrphanPtsMaster:   proto.Bool(true),
			AutoDedup:         proto.Bool(criuOpts.AutoDedup),
			LazyPages:         proto.Bool(criuOpts.LazyPages),
			External:          criuOpts.External,
			MntnsCompatMode:   proto.Bool(criuOpts.MntnsCompatMode),
			TcpClose:          proto.Bool(criuOpts.TcpClose),
			ManageCgroupsMode: (*criurpc.CriuCgMode)(proto.Int32(0)),
		},
	}

	// Same as during checkpointing. If the container has a specific network namespace
	// assigned to it, this now expects that the checkpoint will be restored in a
	// already created network namespace.
	// TODO BS pull this dynamically from original container
	pidStr := ""
	if netPid != 0 {
		pidStr = fmt.Sprint(netPid)
	} else {
		var pidBytes []byte
		var readfileErr error
		if _, err = os.Stat(crioPidFilePath); err == nil {
			pidBytes, readfileErr = os.ReadFile(crioPidFilePath)
		} else if _, err = os.Stat(containerdPidFilePath); err == nil {
			pidBytes, readfileErr = os.ReadFile(containerdPidFilePath)
		}

		if readfileErr != nil {
			return readfileErr
		}

		if pidBytes != nil {
			pidStr = string(pidBytes)
		}
	}

	if pidStr != "" {
		nsPath := fmt.Sprintf("/proc/%s/ns/net", pidStr)
		// For this to work we need at least criu 3.11.0 => 31100.
		// As there was already a successful version check we will
		// not error out if it fails. runc will just behave as it used
		// to do and ignore external network namespaces.
		err := c.checkCriuVersion(31100)
		if err == nil {
			// CRIU wants the information about an existing network namespace
			// like this: --inherit-fd fd[<fd>]:<key>
			// The <key> needs to be the same as during checkpointing.
			// We are always using 'extRootNetNS' as the key in this.
			netns, err := os.Open(nsPath)
			if err != nil {
				return fmt.Errorf("Requested network namespace %v does not exist", nsPath)
			}
			defer netns.Close()
			inheritFd := new(criurpc.InheritFd)
			inheritFd.Key = proto.String("extRootNetNS")
			// The offset of four is necessary because 0, 1, 2 and 3 is already
			// used by stdin, stdout, stderr, 'criu swrk' socket.
			inheritFd.Fd = proto.Int32(int32(4 + len(extraFiles)))
			req.Opts.InheritFd = append(req.Opts.InheritFd, inheritFd)
			// All open FDs need to be transferred to CRIU via extraFiles
			extraFiles = append(extraFiles, netns)
		}
	}

	if criuOpts.LsmProfile != "" {
		// CRIU older than 3.16 has a bug which breaks the possibility
		// to set a different LSM profile.
		if err := c.checkCriuVersion(31600); err != nil {
			return errors.New("--lsm-profile requires at least CRIU 3.16")
		}
		req.Opts.LsmProfile = proto.String(criuOpts.LsmProfile)
	}
	if criuOpts.LsmMountContext != "" {
		if err := c.checkCriuVersion(31600); err != nil {
			return errors.New("--lsm-mount-context requires at least CRIU 3.16")
		}
		req.Opts.LsmMountContext = proto.String(criuOpts.LsmMountContext)
	}

	if criuOpts.WorkDirectory != "" {
		// Since a container can be C/R'ed multiple times,
		// the work directory may already exist.
		if err := os.Mkdir(criuOpts.WorkDirectory, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		workDir, err := os.Open(criuOpts.WorkDirectory)
		if err != nil {
			return err
		}
		defer workDir.Close()
		req.Opts.WorkDirFd = proto.Int32(int32(workDir.Fd()))
		logDir = criuOpts.WorkDirectory
	}
	c.handleCriuConfigurationFile(req.Opts)

	if err := c.handleRestoringNamespaces(req.Opts, &extraFiles, pidStr); err != nil {
		return err
	}

	// This will modify the rootfs of the container in the same way runc
	// modifies the container during initial creation.
	if err := c.prepareCriuRestoreMounts(c.Config.Mounts); err != nil {
		return err
	}

	hasCgroupns := c.Config.Namespaces.Contains(configs.NEWCGROUP)
	for _, m := range c.Config.Mounts {
		switch m.Device {
		case "bind":
			c.addCriuRestoreMount(req, m)
		case "cgroup":
			if cgroups.IsCgroup2UnifiedMode() || hasCgroupns {
				continue
			}
			// cgroup v1 is a set of bind mounts, unless cgroupns is used
			binds, err := getCgroupMounts(m)
			if err != nil {
				return err
			}
			for _, b := range binds {
				c.addCriuRestoreMount(req, b)
			}
		}
	}

	if len(c.Config.MaskPaths) > 0 {
		m := &configs.Mount{Destination: "/dev/null", Source: "/dev/null"}
		c.addCriuRestoreMount(req, m)
	}

	for _, node := range c.Config.Devices {
		m := &configs.Mount{Destination: node.Path, Source: node.Path}
		c.addCriuRestoreMount(req, m)
	}

	if criuOpts.EmptyNs&unix.CLONE_NEWNET == 0 {
		c.restoreNetwork(req, criuOpts)
	}

	// append optional manage cgroups mode
	if criuOpts.ManageCgroupsMode != 0 {
		mode := criuOpts.ManageCgroupsMode
		req.Opts.ManageCgroupsMode = &mode
	}

	var (
		fds    []string
		fdJSON []byte
	)
	if fdJSON, err = os.ReadFile(filepath.Join(criuOpts.ImagesDirectory, descriptorsFilename)); err != nil {
		return err
	}

	if err := json.Unmarshal(fdJSON, &fds); err != nil {
		return err
	}
	for i := range fds {
		if s := fds[i]; strings.Contains(s, "pipe:") {
			inheritFd := new(criurpc.InheritFd)
			inheritFd.Key = proto.String(s)
			inheritFd.Fd = proto.Int32(int32(i))
			req.Opts.InheritFd = append(req.Opts.InheritFd, inheritFd)
		}
	}
	err = c.criuSwrk(process, req, criuOpts, extraFiles, netPid)
	if err != nil {
		logCriuErrors(logDir, logFile)
	}

	// Now that CRIU is done let's close all opened FDs CRIU needed.
	for _, fd := range extraFiles {
		fd.Close()
	}

	return err
}

type Runner struct {
	init            bool
	enableSubreaper bool
	shouldDestroy   bool
	detach          bool
	listenFDs       []*os.File
	preserveFDs     int
	pidFile         string
	consoleSocket   string
	container       *RuncContainer
	action          CtAct
	notifySocket    *notifySocket
	criuOpts        *CriuOpts
	subCgroupPaths  map[string]string
	bundle          string
	netPid          int
}

func (r *Runner) Run(config *specs.Process, runcRoot string) (int, error) {
	var err error
	defer func() {
		if err != nil {
			r.destroy()
		}
	}()
	if err = r.checkTerminal(config); err != nil {
		return -1, err
	}
	process, err := newProcess(*config)
	if err != nil {
		return -1, err
	}
	process.LogLevel = strconv.Itoa(int(log.Logger.GetLevel()))
	// Populate the fields that come from runner.
	process.Init = r.init
	process.SubCgroupPaths = r.subCgroupPaths
	if len(r.listenFDs) > 0 {
		process.Env = append(process.Env, "LISTEN_FDS="+strconv.Itoa(len(r.listenFDs)), "LISTEN_PID=1")
		process.ExtraFiles = append(process.ExtraFiles, r.listenFDs...)
	}
	baseFd := 3 + len(process.ExtraFiles)
	for i := baseFd; i < baseFd+r.preserveFDs; i++ {
		_, err = os.Stat("/proc/self/fd/" + strconv.Itoa(i))
		if err != nil {
			return -1, fmt.Errorf("unable to stat preserved-fd %d (of %d): %w", i-baseFd, r.preserveFDs, err)
		}
		process.ExtraFiles = append(process.ExtraFiles, os.NewFile(uintptr(i), "PreserveFD:"+strconv.Itoa(i)))
	}
	rootuid, err := r.container.Config.HostRootUID()
	if err != nil {
		return -1, err
	}
	rootgid, err := r.container.Config.HostRootGID()
	if err != nil {
		return -1, err
	}
	detach := r.detach || (r.action == CT_ACT_CREATE)
	// Setting up IO is a two stage process. We need to modify process to deal
	// with detaching containers, and then we get a tty after the container has
	// started.
	handler := newSignalHandler(r.enableSubreaper, r.notifySocket)
	tty, err := setupIO(process, rootuid, rootgid, config.Terminal, detach, r.consoleSocket)
	if err != nil {
		return -1, err
	}
	defer tty.Close()

	switch r.action {
	case CT_ACT_RESTORE:
		err = r.container.Restore(process, r.criuOpts, runcRoot, r.bundle, r.netPid)
	default:
		panic("Unknown action")
	}
	if err != nil {
		return -1, err
	}
	if err = tty.waitConsole(); err != nil {
		r.terminate(process)
		return -1, err
	}
	tty.ClosePostStart()
	if r.pidFile != "" {
		if err = createPidFile(r.pidFile, process); err != nil {
			r.terminate(process)
			return -1, err
		}
	}
	status, err := handler.forward(process, tty, detach)
	if err != nil {
		r.terminate(process)
	}
	if detach {
		return 0, nil
	}
	if err == nil {
		r.destroy()
	}
	return status, err
}

func (r *Runner) destroy() {
	if r.shouldDestroy {
		destroyContainer(r.container)
	}
}

func (r *Runner) terminate(p *Process) {
	_ = p.Signal(unix.SIGKILL)
	_, _ = p.Wait()
}

func (r *Runner) checkTerminal(config *specs.Process) error {
	detach := r.detach || (r.action == CT_ACT_CREATE)
	// Check command-line for sanity.
	if detach && config.Terminal && r.consoleSocket == "" {
		return errors.New("cannot allocate tty if runc will detach without setting console socket")
	}
	if (!detach || !config.Terminal) && r.consoleSocket != "" {
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

func GetContainers(root string) ([]ContainerStateJson, error) {
	list, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var s []ContainerStateJson

	for _, item := range list {
		if !item.IsDir() {
			continue
		}
		st, err := item.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Possible race with runc delete.
				continue
			}
			return nil, err
		}
		// This cast is safe on Linux.
		uid := st.Sys().(*syscall.Stat_t).Uid
		owner, err := user.LookupUid(int(uid))
		if err != nil {
			owner.Name = fmt.Sprintf("#%d", uid)
		}

		container, err := libcontainer.Load(root, item.Name())
		if err != nil {
			fmt.Fprintf(os.Stderr, "load container %s: %v\n", item.Name(), err)
			continue
		}
		containerStatus, err := container.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "status for %s: %v\n", item.Name(), err)
			continue
		}
		state, err := container.State()
		if err != nil {
			fmt.Fprintf(os.Stderr, "state for %s: %v\n", item.Name(), err)
			continue
		}
		pid := state.BaseState.InitProcessPid
		if containerStatus == libcontainer.Stopped {
			pid = 0
		}
		bundle, annotations := utils.Annotations(state.Config.Labels)
		s = append(s, ContainerStateJson{
			Version:        state.BaseState.Config.Version,
			ID:             state.BaseState.ID,
			InitProcessPid: pid,
			Status:         containerStatus.String(),
			Bundle:         bundle,
			Rootfs:         state.BaseState.Config.Rootfs,
			Created:        state.BaseState.Created,
			Annotations:    annotations,
			Owner:          owner.Name,
		})
	}
	return s, nil
}

func StartContainer(context *RuncOpts, action CtAct, criuOpts *CriuOpts) (int, error) {
	spec, err := setupSpec(context)
	if err != nil {
		return -1, err
	}

	notifySocket := NewNotifySocket(context, os.Getenv("NOTIFY_SOCKET"), context.ContainerId)
	if notifySocket != nil {
		notifySocket.setupSpec(spec)
	}

	container, err := CreateContainer(context, context.ContainerId, spec)
	if err != nil {
		return -1, err
	}

	if notifySocket != nil {
		if err := notifySocket.setupSocketDirectory(); err != nil {
			return -1, err
		}
		if action == CT_ACT_RUN {
			if err := notifySocket.bindSocket(); err != nil {
				return -1, err
			}
		}
	}

	// Support on-demand socket activation by passing file descriptors into the container init process.
	listenFDs := []*os.File{}
	if os.Getenv("LISTEN_FDS") != "" {
		listenFDs = activation.Files(false)
	}

	r := &Runner{
		enableSubreaper: !context.NoSubreaper,
		shouldDestroy:   !context.Keep,
		container:       container,
		listenFDs:       listenFDs,
		notifySocket:    notifySocket,
		consoleSocket:   context.ConsoleSocket,
		detach:          context.Detach,
		pidFile:         context.PidFile,
		preserveFDs:     context.PreserveFds,
		action:          action,
		criuOpts:        criuOpts,
		init:            true,
		bundle:          context.Bundle,
		netPid:          context.NetPid,
	}

	return r.Run(spec.Process, context.Root)
}
