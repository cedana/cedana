package container

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/utils"
	"github.com/cedana/runc/libcontainer/cgroups"
	"github.com/cedana/runc/libcontainer/cgroups/manager"
	"github.com/cedana/runc/libcontainer/configs"
	"github.com/cedana/runc/libcontainer/system"
	"github.com/checkpoint-restore/go-criu/v6"
	criurpc "github.com/checkpoint-restore/go-criu/v6/rpc"
	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/api/services/tasks/v1"
	apiTasks "github.com/containerd/containerd/api/services/tasks/v1"
	"github.com/containerd/containerd/api/types"
	containerdTypes "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/diff"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/pkg/epoch"
	"github.com/containerd/containerd/plugin"
	"github.com/containerd/containerd/protobuf"
	ptypes "github.com/containerd/containerd/protobuf/types"
	"github.com/containerd/containerd/rootfs"
	"github.com/containerd/containerd/runtime/linux/runctypes"
	"github.com/containerd/containerd/runtime/v2/runc/options"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/platforms"
	"github.com/containerd/typeurl/v2"
	securejoin "github.com/cyphar/filepath-securejoin"
	dockerTypes "github.com/docker/docker/api/types"

	errdefs "github.com/containerd/errdefs"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/identity"
	"github.com/opencontainers/image-spec/specs-go"
	is "github.com/opencontainers/image-spec/specs-go"
	ver "github.com/opencontainers/image-spec/specs-go"
	imagespec "github.com/opencontainers/image-spec/specs-go/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

var (
	ErrUnknown            = errors.New("unknown") // used internally to represent a missed mapping.
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrNotFound           = errors.New("not found")
	ErrAlreadyExists      = errors.New("already exists")
	ErrFailedPrecondition = errors.New("failed precondition")
	ErrUnavailable        = errors.New("unavailable")
	ErrNotImplemented     = errors.New("not implemented") // represents not supported and unimplemented
)

const (
	checkpointImageNameLabel       = "org.opencontainers.image.ref.name"
	checkpointRuntimeNameLabel     = "io.containerd.checkpoint.runtime"
	checkpointSnapshotterNameLabel = "io.containerd.checkpoint.snapshotter"
)

const descriptorsFilename = "descriptors.json"

const (
	checkpointDateFormat = "01-02-2006-15:04:05"
	checkpointNameFormat = "containerd.io/checkpoint/%s:%s"
)

type CheckpointTaskOpts func(*CheckpointTaskInfo) error

// CheckpointTaskInfo allows specific checkpoint information to be set for the task
type CheckpointTaskInfo struct {
	Name string
	// ParentCheckpoint is the digest of a parent checkpoint
	ParentCheckpoint digest.Digest
	// Options hold runtime specific settings for checkpointing a task
	Options interface{}

	runtime string
}

// Runtime name for the container
func (i *CheckpointTaskInfo) Runtime() string {
	return i.runtime
}

type parentProcess interface {
	// pid returns the pid for the running process.
	pid() int

	// start starts the process execution.
	start() error

	// send a SIGKILL to the process and wait for the exit.
	terminate() error

	// wait waits on the process returning the process state.
	wait() (*os.ProcessState, error)

	// startTime returns the process start time.
	startTime() (uint64, error)
	signal(os.Signal) error
	externalDescriptors() []string
	setExternalDescriptors(fds []string)
	forwardChildLogs() chan error
}

type nonChildProcess struct {
	processPid       int
	processStartTime uint64
	fds              []string
}

func (p *nonChildProcess) start() error {
	return errors.New("restored process cannot be started")
}

func (p *nonChildProcess) pid() int {
	return p.processPid
}

func (p *nonChildProcess) terminate() error {
	return errors.New("restored process cannot be terminated")
}

func (p *nonChildProcess) wait() (*os.ProcessState, error) {
	return nil, errors.New("restored process cannot be waited on")
}

func (p *nonChildProcess) startTime() (uint64, error) {
	return p.processStartTime, nil
}

func (p *nonChildProcess) signal(s os.Signal) error {
	proc, err := os.FindProcess(p.processPid)
	if err != nil {
		return err
	}
	return proc.Signal(s)
}

func (p *nonChildProcess) externalDescriptors() []string {
	return p.fds
}

func (p *nonChildProcess) setExternalDescriptors(newFds []string) {
	p.fds = newFds
}

func (p *nonChildProcess) forwardChildLogs() chan error {
	return nil
}

type Status int

type containerState interface {
	transition(containerState) error
	destroy() error
	status() Status
}

type ContainerStateJson struct {
	// Version is the OCI version for the container
	Version string `json:"ociVersion"`
	// ID is the container ID
	ID string `json:"id"`
	// InitProcessPid is the init process id in the parent namespace
	InitProcessPid int `json:"pid"`
	// Status is the current status of the container, running, paused, ...
	Status string `json:"status"`
	// Bundle is the path on the filesystem to the bundle
	Bundle string `json:"bundle"`
	// Rootfs is a path to a directory containing the container's root filesystem.
	Rootfs string `json:"rootfs"`
	// Created is the unix timestamp for the creation time of the container in UTC
	Created time.Time `json:"created"`
	// Annotations is the user defined annotations added to the config.
	Annotations map[string]string `json:"annotations,omitempty"`
	// The owner of the state directory (the owner of the container).
	Owner string `json:"owner"`
}

type RuncContainer struct {
	Id                   string
	StateDir             string
	Root                 string
	Pid                  int
	Config               *configs.Config // standin for configs.Config from runc
	CgroupManager        cgroups.Manager
	InitProcessStartTime uint64
	InitProcess          parentProcess
	M                    sync.Mutex
	CriuVersion          int
	Created              time.Time
	DockerConfig         *dockerTypes.ContainerJSON
	IntelRdtManager      *Manager
	State                containerState
}

func (c *RuncContainer) saveState(s *State) (retErr error) {
	tmpFile, err := os.CreateTemp(c.StateDir, "state-")
	if err != nil {
		return err
	}

	defer func() {
		if retErr != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
		}
	}()

	err = utils.WriteJSON(tmpFile, s)
	if err != nil {
		return err
	}
	err = tmpFile.Close()
	if err != nil {
		return err
	}

	stateFilePath := filepath.Join(c.StateDir, stateFilename)
	return os.Rename(tmpFile.Name(), stateFilePath)
}

func (c *RuncContainer) currentState() (*State, error) {
	var (
		startTime           uint64
		externalDescriptors []string
		pid                 = -1
	)
	if c.InitProcess != nil {
		pid = c.InitProcess.pid()
		startTime, _ = c.InitProcess.startTime()
		externalDescriptors = c.InitProcess.externalDescriptors()
	}

	intelRdtPath := ""
	if c.IntelRdtManager != nil {
		intelRdtPath = c.IntelRdtManager.GetPath()
	}
	state := &State{
		BaseState: BaseState{
			ID:                   c.ID(),
			Config:               *c.Config,
			InitProcessPid:       pid,
			InitProcessStartTime: startTime,
			Created:              c.Created,
		},
		Rootless:            c.Config.RootlessEUID && c.Config.RootlessCgroups,
		CgroupPaths:         c.CgroupManager.GetPaths(),
		IntelRdtPath:        intelRdtPath,
		NamespacePaths:      make(map[configs.NamespaceType]string),
		ExternalDescriptors: externalDescriptors,
	}
	if pid > 0 {
		for _, ns := range c.Config.Namespaces {
			state.NamespacePaths[ns.Type] = ns.GetPath(pid)
		}
		for _, nsType := range configs.NamespaceTypes() {
			if !configs.IsNamespaceSupported(nsType) {
				continue
			}
			if _, ok := state.NamespacePaths[nsType]; !ok {
				ns := configs.Namespace{Type: nsType}
				state.NamespacePaths[ns.Type] = ns.GetPath(pid)
			}
		}
	}
	return state, nil
}

func (c *RuncContainer) updateState(process parentProcess) (*State, error) {
	if process != nil {
		c.InitProcess = process
	}
	state, err := c.currentState()
	if err != nil {
		return nil, err
	}
	err = c.saveState(state)
	if err != nil {
		return nil, err
	}
	return state, nil
}

type restoredProcess struct {
	cmd              *exec.Cmd
	processStartTime uint64
	fds              []string
}

func (p *restoredProcess) start() error {
	return errors.New("restored process cannot be started")
}

func (p *restoredProcess) pid() int {
	return p.cmd.Process.Pid
}

func (p *restoredProcess) terminate() error {
	err := p.cmd.Process.Kill()
	if _, werr := p.wait(); err == nil {
		err = werr
	}
	return err
}

func (p *restoredProcess) wait() (*os.ProcessState, error) {
	// TODO: how do we wait on the actual process?
	// maybe use --exec-cmd in criu
	err := p.cmd.Wait()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, err
		}
	}
	st := p.cmd.ProcessState
	return st, nil
}

func (p *restoredProcess) startTime() (uint64, error) {
	return p.processStartTime, nil
}

func (p *restoredProcess) signal(s os.Signal) error {
	return p.cmd.Process.Signal(s)
}

func (p *restoredProcess) externalDescriptors() []string {
	return p.fds
}

func (p *restoredProcess) setExternalDescriptors(newFds []string) {
	p.fds = newFds
}

func (p *restoredProcess) forwardChildLogs() chan error {
	return nil
}

func newRestoredProcess(cmd *exec.Cmd, fds []string) (*restoredProcess, error) {
	var err error
	pid := cmd.Process.Pid
	stat, err := system.Stat(pid)
	if err != nil {
		return nil, err
	}
	return &restoredProcess{
		cmd:              cmd,
		processStartTime: stat.StartTime,
		fds:              fds,
	}, nil
}

func (c *RuncContainer) criuNotifications(resp *criurpc.CriuResp, process *Process, cmd *exec.Cmd, opts *CriuOpts, fds []string, oob []byte) error {
	notify := resp.GetNotify()
	if notify == nil {
		return fmt.Errorf("invalid response: %s", resp.String())
	}
	script := notify.GetScript()
	logrus.Debugf("notify: %s\n", script)
	switch script {
	case "post-dump":
	case "network-unlock":
		if err := unlockNetwork(c.Config); err != nil {
			return err
		}
	case "network-lock":
		if err := lockNetwork(c.Config); err != nil {
			return err
		}
	case "setup-namespaces":
		if c.Config.Hooks != nil {
			s, err := c.currentOCIState()
			if err != nil {
				return nil
			}
			s.Pid = int(notify.GetPid())

			if err := c.Config.Hooks.Run(configs.Prestart, s); err != nil {
				return err
			}
			if err := c.Config.Hooks.Run(configs.CreateRuntime, s); err != nil {
				return err
			}
		}
	case "post-restore":
		pid := notify.GetPid()

		p, err := os.FindProcess(int(pid))
		if err != nil {
			return err
		}
		cmd.Process = p

		r, err := newRestoredProcess(cmd, fds)
		if err != nil {
			return err
		}
		process.ops = r
		if err := c.State.transition(&restoredState{
			imageDir: opts.ImagesDirectory,
			c:        c,
		}); err != nil {
			return err
		}
		// create a timestamp indicating when the restored checkpoint was started
		c.Created = time.Now().UTC()
		if _, err := c.updateState(r); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(c.StateDir, "checkpoint")); err != nil {
			if !os.IsNotExist(err) {
				logrus.Error(err)
			}
		}
	case "orphan-pts-master":
		scm, err := unix.ParseSocketControlMessage(oob)
		if err != nil {
			return err
		}
		fds, err := unix.ParseUnixRights(&scm[0])
		if err != nil {
			return err
		}

		master := os.NewFile(uintptr(fds[0]), "orphan-pts-master")
		defer master.Close()

		// While we can access console.master, using the API is a good idea.
		if err := utils.SendFile(process.ConsoleSocket, master); err != nil {
			return err
		}
	case "status-ready":
		if opts.StatusFd != -1 {
			// write \0 to status fd to notify that lazy page server is ready
			_, err := unix.Write(opts.StatusFd, []byte{0})
			if err != nil {
				logrus.Warnf("can't write \\0 to status fd: %v", err)
			}
			_ = unix.Close(opts.StatusFd)
			opts.StatusFd = -1
		}
	}
	return nil
}

// this comes from runc, see github.com/cedana/runc
// they use an external CriuOpts struct that's populated
type VethPairName struct {
	ContainerInterfaceName string
	HostInterfaceName      string
}

// Higher level CriuOptions that are used to turn on/off the flags passed to criu
type CriuOpts struct {
	ImagesDirectory         string             // directory for storing image files
	WorkDirectory           string             // directory to cd and write logs/pidfiles/stats to
	ParentImage             string             // directory for storing parent image files in pre-dump and dump
	LeaveRunning            bool               // leave container in running state after checkpoint
	TcpEstablished          bool               // checkpoint/restore established TCP connections
	ExternalUnixConnections bool               // allow external unix connections
	ShellJob                bool               // allow to dump and restore shell jobs
	FileLocks               bool               // handle file locks, for safety
	PreDump                 bool               // call criu predump to perform iterative checkpoint
	VethPairs               []VethPairName     // pass the veth to criu when restore
	ManageCgroupsMode       criurpc.CriuCgMode // dump or restore cgroup mode
	EmptyNs                 uint32             // don't c/r properties for namespace from this mask
	AutoDedup               bool               // auto deduplication for incremental dumps
	LazyPages               bool               // restore memory pages lazily using userfaultfd
	StatusFd                int                // fd for feedback when lazy server is ready
	LsmProfile              string             // LSM profile used to restore the container
	LsmMountContext         string             // LSM mount context value to use during restore
	External                []string           // ignore external namespaces
	MntnsCompatMode         bool
	TcpClose                bool
	TCPInFlight             bool
}

// func (n *loadedState) destroy() error {
// 	if err := n.c.refreshState(); err != nil {
// 		return err
// 	}
// 	return n.c.state.destroy()
// }

func GetContainerFromRunc(containerID string, root string) *RuncContainer {
	// Runc root
	// root := "/var/run/runc"
	// Docker root
	// root := "/run/docker/runtime-runc/moby"
	// Containerd root where "default" is the namespace
	criu := criu.MakeCriu()
	criuVersion, err := criu.GetCriuVersion()
	if err != nil {
		log.Fatal().Err(err).Msg("could not get criu version")
	}
	root = root + "/" + containerID
	state, err := loadState(root)
	if err != nil {
		log.Fatal().Err(err).Msg("could not load state")
	}

	r := &nonChildProcess{
		processPid:       state.InitProcessPid,
		processStartTime: state.InitProcessStartTime,
		fds:              state.ExternalDescriptors,
	}

	cgroupManager, err := manager.NewWithPaths(state.Config.Cgroups, state.CgroupPaths)
	if err != nil {
		log.Fatal().Err(err).Msg("could not create cgroup manager")
	}

	c := &RuncContainer{
		InitProcess:          r,
		InitProcessStartTime: state.InitProcessStartTime,
		Id:                   containerID,
		Root:                 root,
		CriuVersion:          criuVersion,
		CgroupManager:        cgroupManager,
		// dockerConfig:  &container,
		Config:          &state.Config,
		IntelRdtManager: NewManager(&state.Config, containerID, state.IntelRdtPath),
		Pid:             state.InitProcessPid,
		// state:           containerState,
		Created: state.Created,
	}

	// c.state = &loadedState{c: c}
	// if err := c.refreshState(); err != nil {
	// 	return nil, err
	// }
	return c
}

type BaseState struct {
	// ID is the container ID.
	ID string `json:"id"`

	// InitProcessPid is the init process id in the parent namespace.
	InitProcessPid int `json:"init_process_pid"`

	// InitProcessStartTime is the init process start time in clock cycles since boot time.
	InitProcessStartTime uint64 `json:"init_process_start"`

	// Created is the unix timestamp for the creation time of the container in UTC
	Created time.Time `json:"created"`

	// Config is the container's configuration.
	Config configs.Config `json:"config"`
}

type State struct {
	BaseState

	// Platform specific fields below here

	// Specified if the container was started under the rootless mode.
	// Set to true if BaseState.Config.RootlessEUID && BaseState.Config.RootlessCgroups
	Rootless bool `json:"rootless"`

	// Paths to all the container's cgroups, as returned by (*cgroups.Manager).GetPaths
	//
	// For cgroup v1, a key is cgroup subsystem name, and the value is the path
	// to the cgroup for this subsystem.
	//
	// For cgroup v2 unified hierarchy, a key is "", and the value is the unified path.
	CgroupPaths map[string]string `json:"cgroup_paths"`

	// NamespacePaths are filepaths to the container's namespaces. Key is the namespace type
	// with the value as the path.
	NamespacePaths map[configs.NamespaceType]string `json:"namespace_paths"`

	// Container's standard descriptors (std{in,out,err}), needed for checkpoint and restore
	ExternalDescriptors []string `json:"external_descriptors,omitempty"`

	// Intel RDT "resource control" filesystem path
	IntelRdtPath string `json:"intel_rdt_path"`
}

func loadState(root string) (*State, error) {
	stateFilePath, err := securejoin.SecureJoin(root, "state.json")
	if err != nil {
		return nil, err
	}
	f, err := os.Open(stateFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, err
	}
	defer f.Close()
	var state *State
	if err := json.NewDecoder(f).Decode(&state); err != nil {
		return nil, err
	}
	return state, nil
}

func newContainerdClient(ctx context.Context, opts ...containerd.ClientOpt) (*containerd.Client, context.Context, context.CancelFunc, error) {
	timeoutOpt := containerd.WithTimeout(0)
	containerdEndpoint := "/run/containerd/containerd.sock"
	if _, err := os.Stat(containerdEndpoint); err != nil {
		containerdEndpoint = "/host/run/k3s/containerd/containerd.sock"
	}
	opts = append(opts, timeoutOpt)

	client, err := containerd.New(containerdEndpoint, opts...)
	if err != nil {
		fmt.Print("failed to create client")
	}
	ctx, cancel := AppContext(ctx)
	return client, ctx, cancel, err
}

// AppContext returns the context for a command. Should only be called once per
// command, near the start.
//
// This will ensure the namespace is picked up and set the timeout, if one is
// defined.
func AppContext(ctx context.Context) (context.Context, context.CancelFunc) {
	var (
		timeout   = 0
		namespace = "k8s.io"
		cancel    context.CancelFunc
	)
	ctx = namespaces.WithNamespace(ctx, namespace)
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	if tm, err := epoch.SourceDateEpoch(); err != nil {
		log.Warn().Err(err).Msg("Failed to read SOURCE_DATE_EPOCH")
	} else if tm != nil {
		log.Debug().Msgf("Using SOURCE_DATE_EPOCH: %v", tm)
		ctx = epoch.WithSourceDateEpoch(ctx, tm)
	}
	return ctx, cancel
}

func containerdCheckpoint(imagePath, id string) error {
	ctx := context.Background()

	containerdClient, ctx, cancel, err := newContainerdClient(ctx)
	if err != nil {
		log.Fatal().Err(err)
	}
	defer cancel()

	containers, err := containerdClient.Containers(ctx)
	if err != nil {
		return err
	}
	for _, container := range containers {
		fmt.Println(container.ID())
	}

	opts := []containerd.CheckpointOpts{
		containerd.WithCheckpointRuntime,
	}

	opts = append(opts, containerd.WithCheckpointImage)
	opts = append(opts, containerd.WithCheckpointRW)
	opts = append(opts, containerd.WithCheckpointTask)

	container, err := containerdClient.LoadContainer(ctx, id)
	if err != nil {
		return err
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}
	}
	// pause if running
	if task != nil {
		if err := task.Pause(ctx); err != nil {
			return err
		}
		defer func() {
			if err := task.Resume(ctx); err != nil {
				fmt.Println(fmt.Errorf("error resuming task: %w", err))
			}
		}()
	}

	// checkpoint task
	// checkpoint, err := task.Checkpoint(ctx, containerd.WithCheckpointImagePath(imagePath)) //checkpoint, err := runcCheckpointContainerd(ctx, containerdClient, task, WithCheckpointImagePath(""))
	checkpoint, err := container.Checkpoint(ctx, "test123", opts...)
	if err != nil {
		return err
	}

	fmt.Printf("Checkpoint name: %s\n", checkpoint.Name())

	// if _, err := container.Checkpoint(ctx, ref, containerdOpts...); err != nil {
	// 	return err
	// }

	return nil
}

// getCheckpointPath only suitable for runc runtime now
func getCheckpointPath(runtime string, option *ptypes.Any) (string, error) {
	if option == nil {
		return "", nil
	}

	var checkpointPath string
	v, err := typeurl.UnmarshalAny(option)
	if err != nil {
		return "", err
	}
	opts, ok := v.(*options.CheckpointOptions)
	if !ok {
		return "", fmt.Errorf("invalid task checkpoint option for %s", runtime)
	}
	checkpointPath = opts.ImagePath

	return checkpointPath, nil
}

func localCheckpointTask(ctx context.Context, client *containerd.Client, index *v1.Index, request *apiTasks.CheckpointTaskRequest, container containers.Container) (*apiTasks.CheckpointTaskResponse, error) {
	// TODO BS get rid of marshal/unmarshal & CTR
	v, err := typeurl.UnmarshalAny(request.Options)
	if err != nil {
		return &apiTasks.CheckpointTaskResponse{}, err
	}
	opts, ok := v.(*options.CheckpointOptions)
	if !ok {
		return &apiTasks.CheckpointTaskResponse{}, fmt.Errorf("invalid task checkpoint option for %s", container.Runtime.Name)
	}

	image := opts.ImagePath

	if image == "" {
		image, err = os.MkdirTemp(os.Getenv("XDG_RUNTIME_DIR"), "ctrd-checkpoint")
		if err != nil {
			return &apiTasks.CheckpointTaskResponse{}, err
		}

		fmt.Printf("Checkpointing to %s\n", image)
		defer os.RemoveAll(image)
	}

	// write the config to the content store
	pbany := protobuf.FromAny(container.Spec)
	data, err := proto.Marshal(pbany)
	if err != nil {
		return &apiTasks.CheckpointTaskResponse{}, err
	}
	if err := os.WriteFile(filepath.Join(image, "spec"), data, 0777); err != nil {
		return &apiTasks.CheckpointTaskResponse{}, err
	}
	spec := bytes.NewReader(data)
	specD, err := localWriteContent(ctx, client, images.MediaTypeContainerd1CheckpointConfig, filepath.Join(image, "spec"), spec)
	if err != nil {
		return &apiTasks.CheckpointTaskResponse{}, err
	}
	return &apiTasks.CheckpointTaskResponse{
		Descriptors: []*containerdTypes.Descriptor{
			specD,
		},
	}, nil
}

func localWriteContent(ctx context.Context, client *containerd.Client, mediaType, ref string, r io.Reader) (*types.Descriptor, error) {
	writer, err := client.ContentStore().Writer(ctx, content.WithRef(ref), content.WithDescriptor(ocispec.Descriptor{MediaType: mediaType}))
	if err != nil {
		return nil, err
	}
	defer writer.Close()
	size, err := io.Copy(writer, r)
	if err != nil {
		return nil, err
	}
	if err := writer.Commit(ctx, 0, ""); err != nil {
		return nil, err
	}
	return &types.Descriptor{
		MediaType:   mediaType,
		Digest:      writer.Digest().String(),
		Size:        size,
		Annotations: make(map[string]string),
	}, nil
}

// WithCheckpointImagePath sets image path for checkpoint option
func WithCheckpointImagePath(path string) CheckpointTaskOpts {
	return func(r *CheckpointTaskInfo) error {
		if CheckRuntime(r.Runtime(), "io.containerd.runc") {
			if r.Options == nil {
				r.Options = &options.CheckpointOptions{}
			}
			opts, ok := r.Options.(*options.CheckpointOptions)
			if !ok {
				return errors.New("invalid v2 shim checkpoint options format")
			}
			opts.ImagePath = path
		} else {
			if r.Options == nil {
				r.Options = &runctypes.CheckpointOptions{}
			}
			opts, ok := r.Options.(*runctypes.CheckpointOptions)
			if !ok {
				return errors.New("invalid v1 shim checkpoint options format")
			}
			opts.ImagePath = path
		}
		return nil
	}
}

// CheckRuntime returns true if the current runtime matches the expected
// runtime. Providing various parts of the runtime schema will match those
// parts of the expected runtime
func CheckRuntime(current, expected string) bool {
	cp := strings.Split(current, ".")
	l := len(cp)
	for i, p := range strings.Split(expected, ".") {
		if i > l {
			return false
		}
		if p != cp[i] {
			return false
		}
	}
	return true
}

type ContainerdContainer struct {
	containerd.Container
	client *containerd.Client
}

var (
	emptyGZLayer = digest.Digest("sha256:4f4fb700ef54461cfa02571ae0db9a0dc1e0cdb5577484a6d75e68dc38e8acc1")
	emptyDigest  = digest.Digest("")
)

// for a full containerd checkpoint, we'd use the runc checkpointing primitives + rootfs
func ContainerdRootfsCheckpoint(ctx context.Context, containerdClient *containerd.Client, id, ref string) error {
	containers, err := containerdClient.Containers(ctx)
	if err != nil {
		return err
	}
	for _, container := range containers {
		fmt.Println(container.ID())
	}

	container, err := containerdClient.LoadContainer(ctx, id)
	if err != nil {
		return err
	}

	containerdContainer := &ContainerdContainer{
		Container: container,
		client:    containerdClient,
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}
	}
	// pause if running
	if task != nil {
		currentStatus, err := task.Status(ctx)
		if err != nil {
			return err
		}

		if currentStatus.Status != "paused" {
			if err := task.Pause(ctx); err != nil {
				return err
			}

			defer func() {
				if err := task.Resume(ctx); err != nil {
					fmt.Println(fmt.Errorf("error resuming task: %w", err))
				}
			}()
		}

	}

	if err := containerdContainer.ContainerCheckpointContainerd(ctx, ref); err != nil {
		return err
	}

	return nil
}

func WithCheckpointState(ctx context.Context, client *containerd.Client, c *containers.Container, index *imagespec.Index, copts *options.CheckpointOptions) error {
	any, err := protobuf.MarshalAnyToProto(copts)
	if err != nil {
		return nil
	}

	request := &tasks.CheckpointTaskRequest{
		ContainerID: c.ID,
		Options:     any,
	}

	response, err := localCheckpointTask(ctx, client, index, request, *c)
	if err != nil {
		return err
	}

	for _, d := range response.Descriptors {
		index.Manifests = append(index.Manifests, v1.Descriptor{
			MediaType: d.MediaType,
			Size:      d.Size,
			Digest:    digest.Digest(d.Digest),
			Platform: &v1.Platform{
				OS:           runtime.GOOS,
				Architecture: runtime.GOARCH,
			},
			Annotations: d.Annotations,
		})
	}

	return nil
}

var Platform = "cedana/platform"

func (c *ContainerdContainer) ContainerCheckpointContainerd(ctx context.Context, ref string) error {
	info, err := c.Info(ctx)
	if err != nil {
		return err
	}

	baseImgNoPlatform, err := c.client.ImageService().Get(ctx, info.Image)
	if err != nil {
		return fmt.Errorf("container %q lacks image: %w", c.Container.ID(), err)
	}

	ctx, done, err := c.client.WithLease(ctx)
	if err != nil {
		return err
	}
	defer done(ctx)

	platformLabel := info.Labels[Platform]
	if platformLabel == "" {
		platformLabel = platforms.DefaultString()
	}

	ocispecPlatform, err := platforms.Parse(platformLabel)
	if err != nil {
		return err
	}

	platform := platforms.Only(ocispecPlatform)

	baseImg := containerd.NewImageWithPlatform(c.client, baseImgNoPlatform, platform)

	baseImgConfig, _, err := ReadImageConfig(ctx, baseImg)
	if err != nil {
		return err
	}

	var (
		differ = c.client.DiffService()
		snName = info.Snapshotter
		sn     = c.client.SnapshotService(snName)
	)

	diffLayerDesc, diffID, err := createDiff(ctx, c.Container.ID(), sn, c.client.ContentStore(), differ)
	if err != nil {
		return fmt.Errorf("failed to export layer: %w", err)
	}

	imageConfig, err := generateCommitImageConfig(ctx, c.Container, baseImg, diffID)
	if err != nil {
		return fmt.Errorf("failed to generate commit image config: %w", err)
	}

	rootfsID := identity.ChainID(imageConfig.RootFS.DiffIDs).String()
	if err := applyDiffLayer(ctx, rootfsID, baseImgConfig, sn, differ, diffLayerDesc); err != nil {
		return fmt.Errorf("failed to apply diff: %w", err)
	}

	commitManifestDesc, _, err := writeContentsForImage(ctx, snName, baseImg, imageConfig, diffLayerDesc)
	if err != nil {
		return err
	}

	// image create
	img := images.Image{
		Name:      ref,
		Target:    commitManifestDesc,
		CreatedAt: time.Now(),
	}

	if _, err := c.client.ImageService().Update(ctx, img); err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}

		if _, err := c.client.ImageService().Create(ctx, img); err != nil {
			return fmt.Errorf("failed to create new image %s: %w", ref, err)
		}
	}

	return nil
}

func ReadImageConfig(ctx context.Context, img containerd.Image) (ocispec.Image, ocispec.Descriptor, error) {
	var config ocispec.Image

	configDesc, err := img.Config(ctx) // aware of img.platform
	if err != nil {
		return config, configDesc, err
	}
	p, err := content.ReadBlob(ctx, img.ContentStore(), configDesc)
	if err != nil {
		return config, configDesc, err
	}
	if err := json.Unmarshal(p, &config); err != nil {
		return config, configDesc, err
	}
	return config, configDesc, nil
}

func ReadManifest(ctx context.Context, img containerd.Image) (*ocispec.Manifest, *ocispec.Descriptor, error) {
	cs := img.ContentStore()
	targetDesc := img.Target()
	if images.IsManifestType(targetDesc.MediaType) {
		b, err := content.ReadBlob(ctx, img.ContentStore(), targetDesc)
		if err != nil {
			return nil, &targetDesc, err
		}
		var mani ocispec.Manifest
		if err := json.Unmarshal(b, &mani); err != nil {
			return nil, &targetDesc, err
		}
		return &mani, &targetDesc, nil
	}
	if images.IsIndexType(targetDesc.MediaType) {
		idx, _, err := ReadIndex(ctx, img)
		if err != nil {
			return nil, nil, err
		}
		configDesc, err := img.Config(ctx) // aware of img.platform
		if err != nil {
			return nil, nil, err
		}

		for _, maniDesc := range idx.Manifests {
			maniDesc := maniDesc
			if b, err := content.ReadBlob(ctx, cs, maniDesc); err == nil {
				var mani ocispec.Manifest
				if err := json.Unmarshal(b, &mani); err != nil {
					return nil, nil, err
				}
				if reflect.DeepEqual(configDesc, mani.Config) {
					return &mani, &maniDesc, nil
				}
			}
		}
	}
	// no manifest was found
	return nil, nil, nil
}

// ReadIndex returns image index, or nil for non-indexed image.
func ReadIndex(ctx context.Context, img containerd.Image) (*ocispec.Index, *ocispec.Descriptor, error) {
	desc := img.Target()
	if !images.IsIndexType(desc.MediaType) {
		return nil, nil, nil
	}
	b, err := content.ReadBlob(ctx, img.ContentStore(), desc)
	if err != nil {
		return nil, &desc, err
	}
	var idx ocispec.Index
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, &desc, err
	}

	return &idx, &desc, nil
}

// writeContentsForImage will commit oci image config and manifest into containerd's content store.
func writeContentsForImage(ctx context.Context, snName string, baseImg containerd.Image, newConfig ocispec.Image, diffLayerDesc ocispec.Descriptor) (ocispec.Descriptor, digest.Digest, error) {
	newConfigJSON, err := json.Marshal(newConfig)
	if err != nil {
		return ocispec.Descriptor{}, emptyDigest, err
	}

	configDesc := ocispec.Descriptor{
		MediaType: images.MediaTypeDockerSchema2Config,
		Digest:    digest.FromBytes(newConfigJSON),
		Size:      int64(len(newConfigJSON)),
	}

	cs := baseImg.ContentStore()
	baseMfst, _, err := ReadManifest(ctx, baseImg)
	if err != nil {
		return ocispec.Descriptor{}, emptyDigest, err
	}
	layers := append(baseMfst.Layers, diffLayerDesc)

	newMfst := struct {
		MediaType string `json:"mediaType,omitempty"`
		ocispec.Manifest
	}{
		MediaType: images.MediaTypeDockerSchema2Manifest,
		Manifest: ocispec.Manifest{
			Versioned: specs.Versioned{
				SchemaVersion: 2,
			},
			Config: configDesc,
			Layers: layers,
		},
	}

	newMfstJSON, err := json.MarshalIndent(newMfst, "", "    ")
	if err != nil {
		return ocispec.Descriptor{}, emptyDigest, err
	}

	newMfstDesc := ocispec.Descriptor{
		MediaType: images.MediaTypeDockerSchema2Manifest,
		Digest:    digest.FromBytes(newMfstJSON),
		Size:      int64(len(newMfstJSON)),
	}

	// new manifest should reference the layers and config content
	labels := map[string]string{
		"containerd.io/gc.ref.content.0": configDesc.Digest.String(),
	}
	for i, l := range layers {
		labels[fmt.Sprintf("containerd.io/gc.ref.content.%d", i+1)] = l.Digest.String()
	}

	err = content.WriteBlob(ctx, cs, newMfstDesc.Digest.String(), bytes.NewReader(newMfstJSON), newMfstDesc, content.WithLabels(labels))
	if err != nil {
		return ocispec.Descriptor{}, emptyDigest, err
	}

	// config should reference to snapshotter
	labelOpt := content.WithLabels(map[string]string{
		fmt.Sprintf("containerd.io/gc.ref.snapshot.%s", snName): identity.ChainID(newConfig.RootFS.DiffIDs).String(),
	})
	err = content.WriteBlob(ctx, cs, configDesc.Digest.String(), bytes.NewReader(newConfigJSON), configDesc, labelOpt)
	if err != nil {
		return ocispec.Descriptor{}, emptyDigest, err
	}

	return newMfstDesc, configDesc.Digest, nil
}

func generateCommitImageConfig(ctx context.Context, container containerd.Container, img containerd.Image, diffID digest.Digest) (ocispec.Image, error) {
	spec, err := container.Spec(ctx)
	if err != nil {
		return ocispec.Image{}, err
	}

	baseConfig, _, err := ReadImageConfig(ctx, img) // aware of img.platform
	if err != nil {
		return ocispec.Image{}, err
	}

	createdBy := ""
	if spec.Process != nil {
		createdBy = strings.Join(spec.Process.Args, " ")
	}

	createdTime := time.Now()
	arch := baseConfig.Architecture
	if arch == "" {
		arch = runtime.GOARCH
		// log.G(ctx).Warnf("assuming arch=%q", arch)
	}
	os := baseConfig.OS
	if os == "" {
		os = runtime.GOOS
		// log.G(ctx).Warnf("assuming os=%q", os)
	}
	// log.G(ctx).Debugf("generateCommitImageConfig(): arch=%q, os=%q", arch, os)
	return ocispec.Image{
		Platform: ocispec.Platform{
			Architecture: arch,
			OS:           os,
		},

		Created: &createdTime,
		Author:  "cedana",
		Config:  baseConfig.Config,
		RootFS: ocispec.RootFS{
			Type:    "layers",
			DiffIDs: append(baseConfig.RootFS.DiffIDs, diffID),
		},
		History: append(baseConfig.History, ocispec.History{
			Created:    &createdTime,
			CreatedBy:  createdBy,
			Author:     "cedana",
			Comment:    "",
			EmptyLayer: (diffID == emptyGZLayer),
		}),
	}, nil
}

// createDiff creates a layer diff into containerd's content store.
func createDiff(ctx context.Context, name string, sn snapshots.Snapshotter, cs content.Store, comparer diff.Comparer) (ocispec.Descriptor, digest.Digest, error) {
	newDesc, err := rootfs.CreateDiff(ctx, name, sn, comparer)
	if err != nil {
		return ocispec.Descriptor{}, digest.Digest(""), err
	}

	info, err := cs.Info(ctx, newDesc.Digest)
	if err != nil {
		return ocispec.Descriptor{}, digest.Digest(""), err
	}

	diffIDStr, ok := info.Labels["containerd.io/uncompressed"]
	if !ok {
		return ocispec.Descriptor{}, digest.Digest(""), fmt.Errorf("invalid differ response with no diffID")
	}

	diffID, err := digest.Parse(diffIDStr)
	if err != nil {
		return ocispec.Descriptor{}, digest.Digest(""), err
	}

	return ocispec.Descriptor{
		MediaType: images.MediaTypeDockerSchema2LayerGzip,
		Digest:    newDesc.Digest,
		Size:      info.Size,
	}, diffID, nil
}

// copied from github.com/containerd/containerd/rootfs/apply.go
func uniquePart() string {
	t := time.Now()
	var b [3]byte
	// Ignore read failures, just decreases uniqueness
	rand.Read(b[:])
	return fmt.Sprintf("%d-%s", t.Nanosecond(), base64.URLEncoding.EncodeToString(b[:]))
}

// applyDiffLayer will apply diff layer content created by createDiff into the snapshotter.
func applyDiffLayer(ctx context.Context, name string, baseImg ocispec.Image, sn snapshots.Snapshotter, differ diff.Applier, diffDesc ocispec.Descriptor) (retErr error) {
	var (
		key    = uniquePart() + "-" + name
		parent = identity.ChainID(baseImg.RootFS.DiffIDs).String()
	)

	mount, err := sn.Prepare(ctx, key, parent)
	if err != nil {
		return err
	}

	defer func() {
		if retErr != nil {
			// NOTE: the snapshotter should be hold by lease. Even
			// if the cleanup fails, the containerd gc can delete it.
			if err := sn.Remove(ctx, key); err != nil {
				log.Ctx(ctx).Debug().Msgf("failed to cleanup aborted apply %s: %s", key, err)
			}
		}
	}()

	if _, err = differ.Apply(ctx, diffDesc, mount); err != nil {
		return err
	}

	if err = sn.Commit(ctx, name, key); err != nil {
		if errdefs.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	return nil
}

func RuncCheckpointContainerd(ctx context.Context, client *containerd.Client, task containerd.Task, opts ...CheckpointTaskOpts) (containerd.Image, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// This is for garbage collection
	ctx, done, err := client.WithLease(ctx)
	if err != nil {
		return nil, err
	}
	defer done(ctx)
	cr, err := client.ContainerService().Get(ctx, task.ID())
	if err != nil {
		return nil, err
	}

	request := &apiTasks.CheckpointTaskRequest{
		ContainerID: task.ID(),
	}
	i := CheckpointTaskInfo{
		runtime: cr.Runtime.Name,
	}
	for _, o := range opts {
		if err := o(&i); err != nil {
			return nil, err
		}
	}
	// set a default name
	if i.Name == "" {
		i.Name = fmt.Sprintf(checkpointNameFormat, task.ID(), time.Now().Format(checkpointDateFormat))
	}
	request.ParentCheckpoint = i.ParentCheckpoint.String()
	if i.Options != nil {
		any, err := protobuf.MarshalAnyToProto(i.Options)
		if err != nil {
			return nil, err
		}
		request.Options = any
	}

	status, err := task.Status(ctx)
	if err != nil {
		return nil, err
	}

	if status.Status != containerd.Paused {
		// make sure we pause it and resume after all other filesystem operations are completed
		if err := task.Pause(ctx); err != nil {
			return nil, err
		}
		defer task.Resume(ctx)
	}

	index := v1.Index{
		Versioned: is.Versioned{
			SchemaVersion: 2,
		},
		Annotations: make(map[string]string),
	}
	// TODO: this is where we do custom criu checkpoint
	response, err := localCheckpointTask(ctx, client, &index, request, cr)
	if err != nil {
		return nil, err
	}

	for _, d := range response.Descriptors {
		index.Manifests = append(index.Manifests, v1.Descriptor{
			MediaType: d.MediaType,
			Size:      d.Size,
			Digest:    digest.Digest(d.Digest),
			Platform: &v1.Platform{
				OS:           runtime.GOOS,
				Architecture: runtime.GOARCH,
			},
			Annotations: d.Annotations,
		})
	}
	// if checkpoint image path passed, jump checkpoint image,
	// return an empty image
	if isCheckpointPathExist(cr.Runtime.Name, i.Options) {
		return containerd.NewImage(client, images.Image{}), nil
	}

	// add runtime info to index
	index.Annotations[checkpointRuntimeNameLabel] = cr.Runtime.Name
	// add snapshotter info to index
	index.Annotations[checkpointSnapshotterNameLabel] = cr.Snapshotter

	if cr.Image != "" {
		if err := checkpointImage(ctx, client, &index, cr.Image); err != nil {
			return nil, err
		}
		// Changed this from image.name
		index.Annotations[checkpointImageNameLabel] = cr.Image
	}
	if cr.SnapshotKey != "" {
		if err := checkpointRWSnapshot(ctx, client, &index, cr.Snapshotter, cr.SnapshotKey); err != nil {
			return nil, err
		}
	}
	desc, err := writeIndex(ctx, client, task, &index)
	if err != nil {
		return nil, err
	}
	im := images.Image{
		Name:   i.Name,
		Target: desc,
		Labels: map[string]string{
			"containerd.io/checkpoint": "true",
		},
	}
	if im, err = client.ImageService().Create(ctx, im); err != nil {
		return nil, err
	}
	return containerd.NewImage(client, im), nil
}

func writeIndex(ctx context.Context, client *containerd.Client, task containerd.Task, index *v1.Index) (d v1.Descriptor, err error) {
	labels := map[string]string{}
	for i, m := range index.Manifests {
		labels[fmt.Sprintf("containerd.io/gc.ref.content.%d", i)] = m.Digest.String()
	}
	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(index); err != nil {
		return v1.Descriptor{}, err
	}
	return writeContent(ctx, client.ContentStore(), v1.MediaTypeImageIndex, task.ID(), buf, content.WithLabels(labels))
}

func writeContent(ctx context.Context, store content.Ingester, mediaType, ref string, r io.Reader, opts ...content.Opt) (d v1.Descriptor, err error) {
	writer, err := store.Writer(ctx, content.WithRef(ref))
	if err != nil {
		return d, err
	}
	defer writer.Close()
	size, err := io.Copy(writer, r)
	if err != nil {
		return d, err
	}

	if err := writer.Commit(ctx, size, "", opts...); err != nil {
		if !IsAlreadyExists(err) {
			return d, err
		}
	}
	return v1.Descriptor{
		MediaType: mediaType,
		Digest:    writer.Digest(),
		Size:      size,
	}, nil
}

func IsAlreadyExists(err error) bool {
	return errors.Is(err, ErrAlreadyExists)
}

func checkpointImage(ctx context.Context, client *containerd.Client, index *v1.Index, image string) error {
	if image == "" {
		return fmt.Errorf("cannot checkpoint image with empty name")
	}
	ir, err := client.ImageService().Get(ctx, image)
	if err != nil {
		return err
	}
	index.Manifests = append(index.Manifests, ir.Target)
	return nil
}

func checkpointRWSnapshot(ctx context.Context, client *containerd.Client, index *v1.Index, snapshotterName string, id string) error {
	opts := []diff.Opt{
		diff.WithReference(fmt.Sprintf("checkpoint-rw-%s", id)),
	}
	rw, err := rootfs.CreateDiff(ctx, id, client.SnapshotService(snapshotterName), client.DiffService(), opts...)
	if err != nil {
		return err
	}
	rw.Platform = &v1.Platform{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
	}
	index.Manifests = append(index.Manifests, rw)
	return nil
}

func isCheckpointPathExist(runtime string, v interface{}) bool {
	if v == nil {
		return false
	}

	switch runtime {
	case plugin.RuntimeRuncV1, plugin.RuntimeRuncV2:
		if opts, ok := v.(*options.CheckpointOptions); ok && opts.ImagePath != "" {
			return true
		}

	case plugin.RuntimeLinuxV1:
		if opts, ok := v.(*runctypes.CheckpointOptions); ok && opts.ImagePath != "" {
			return true
		}
	}

	return false
}

func snapshotOpts(id string) error {
	ctx := context.Background()

	copts := &options.CheckpointOptions{
		Exit:                false,
		OpenTcp:             false,
		ExternalUnixSockets: false,
		Terminal:            false,
		FileLocks:           true,
		EmptyNamespaces:     nil,
	}

	opts := []containerd.CheckpointOpts{
		containerd.WithCheckpointRuntime,
		containerd.WithCheckpointImage,
		containerd.WithCheckpointRW,
	}

	containerdClient, err := containerd.New("/run/containerd/containerd.sock")
	if err != nil {
		return err
	}

	index := &ocispec.Index{
		Versioned: ver.Versioned{
			SchemaVersion: 2,
		},
		Annotations: make(map[string]string),
	}
	container, err := containerdClient.LoadContainer(ctx, id)
	if err != nil {
		return err
	}

	info, err := container.Info(ctx)
	if err != nil {
		return err
	}

	img, err := container.Image(ctx)
	if err != nil {
		return err
	}

	// add image name to manifest
	index.Annotations[checkpointImageNameLabel] = img.Name()
	// add runtime info to index
	index.Annotations[checkpointRuntimeNameLabel] = info.Runtime.Name
	// add snapshotter info to index
	index.Annotations[checkpointSnapshotterNameLabel] = info.Snapshotter

	for _, o := range opts {
		if err := o(ctx, containerdClient, &info, index, copts); err != nil {
			err = errdefs.FromGRPC(err)
			if !errdefs.IsAlreadyExists(err) {
				return err
			}
		}
	}

	return nil
}

func (c *RuncContainer) RuncCheckpoint(criuOpts *CriuOpts, pid int, runcRoot string, pauseConfig *configs.Config) error {
	c.M.Lock()
	defer c.M.Unlock()

	// snapshotOpts(c.Id)
	// Checkpoint is unlikely to work if os.Geteuid() != 0 || system.RunningInUserNS().
	// (CLI prints a warning)
	// TODO(avagin): Figure out how to make this work nicely. CRIU 2.0 has
	//               support for doing unprivileged dumps, but the setup of
	//               rootless containers might make this complicated.

	// We are relying on the CRIU version RPC which was introduced with CRIU 3.0.0
	if err := c.checkCriuVersion(30000); err != nil {
		return err
	}

	if criuOpts.ImagesDirectory == "" {
		return errors.New("invalid directory to save checkpoint")
	}

	// Since a container can be C/R'ed multiple times,
	// the checkpoint directory may already exist.
	if err := os.Mkdir(criuOpts.ImagesDirectory, 0o777); err != nil && !os.IsExist(err) {
		return err
	}

	imageDir, err := os.Open(criuOpts.ImagesDirectory)
	if err != nil {
		return err
	}
	defer imageDir.Close()

	// Find all bind mounts and add them as external mountpoints
	for _, m := range c.Config.Mounts {
		if m.IsBind() {
			criuOpts.External = append(criuOpts.External, fmt.Sprintf("mnt[%s]:%s", m.Destination, m.Destination))
		}
	}

	rpcOpts := criurpc.CriuOpts{
		ImagesDirFd:     proto.Int32(int32(imageDir.Fd())),
		LogLevel:        proto.Int32(4),
		LogFile:         proto.String("dump.log"),
		Root:            proto.String(c.Config.Rootfs), // TODO NR:not sure if workingDir is analogous here
		ManageCgroups:   proto.Bool(true),
		NotifyScripts:   proto.Bool(true),
		Pid:             proto.Int32(int32(pid)),
		ShellJob:        proto.Bool(criuOpts.ShellJob),
		LeaveRunning:    proto.Bool(criuOpts.LeaveRunning),
		TcpEstablished:  proto.Bool(criuOpts.TcpEstablished),
		ExtUnixSk:       proto.Bool(criuOpts.ExternalUnixConnections),
		FileLocks:       proto.Bool(criuOpts.FileLocks),
		EmptyNs:         proto.Uint32(criuOpts.EmptyNs),
		OrphanPtsMaster: proto.Bool(true),
		AutoDedup:       proto.Bool(criuOpts.AutoDedup),
		LazyPages:       proto.Bool(criuOpts.LazyPages),
		External:        criuOpts.External,
		TcpSkipInFlight: proto.Bool(criuOpts.TCPInFlight),
	}
	// If the container is running in a network namespace and has
	// a path to the network namespace configured, we will dump
	// that network namespace as an external namespace and we
	// will expect that the namespace exists during restore.
	// This basically means that CRIU will ignore the namespace
	// and expect to be setup correctly.
	nsPath := pauseConfig.Namespaces.PathOf(configs.NEWNET)
	if nsPath != "" {
		// For this to work we need at least criu 3.11.0 => 31100.
		// As there was already a successful version check we will
		// not error out if it fails. runc will just behave as it used
		// to do and ignore external network namespaces.
		err := c.checkCriuVersion(31100)
		if err == nil {
			// CRIU expects the information about an external namespace
			// like this: --external net[<inode>]:<key>
			// This <key> is always 'extRootNetNS'.
			var netns syscall.Stat_t
			err = syscall.Stat(nsPath, &netns)
			if err != nil {
				return err
			}
			criuExternal := fmt.Sprintf("net[%d]:extRootNetNS", netns.Ino)
			rpcOpts.External = append(rpcOpts.External, criuExternal)
		}
	}
	// if criuOpts.WorkDirectory is not set, criu default is used.
	if criuOpts.WorkDirectory != "" {
		if err := os.Mkdir(criuOpts.WorkDirectory, 0o777); err != nil && !os.IsExist(err) {
			return err
		}
		workDir, err := os.Open(criuOpts.WorkDirectory)
		if err != nil {
			return err
		}
		defer workDir.Close()
		rpcOpts.WorkDirFd = proto.Int32(int32(workDir.Fd()))
	}

	// CRIU can use cgroup freezer; when rpcOpts.FreezeCgroup
	// is not set, CRIU uses ptrace() to pause the processes.
	// Note cgroup v2 freezer is only supported since CRIU release 3.14.
	if !cgroups.IsCgroup2UnifiedMode() || c.checkCriuVersion(31400) == nil {
		if fcg := c.CgroupManager.Path("freezer"); fcg != "" {
			rpcOpts.FreezeCgroup = proto.String(fcg)
		}
	}

	// pre-dump may need parentImage param to complete iterative migration
	if criuOpts.ParentImage != "" {
		rpcOpts.ParentImg = proto.String(criuOpts.ParentImage)
		rpcOpts.TrackMem = proto.Bool(true)
	}

	// append optional manage cgroups mode
	if criuOpts.ManageCgroupsMode != 0 {
		mode := criuOpts.ManageCgroupsMode
		rpcOpts.ManageCgroupsMode = &mode
	}

	// pre-dump may need parentImage param to complete iterative migration
	if criuOpts.ParentImage != "" {
		rpcOpts.ParentImg = proto.String(criuOpts.ParentImage)
		rpcOpts.TrackMem = proto.Bool(true)
	}

	var t criurpc.CriuReqType
	if criuOpts.PreDump {
		feat := criurpc.CriuFeatures{
			MemTrack: proto.Bool(true),
		}

		if err := c.checkCriuFeatures(criuOpts, &rpcOpts, &feat); err != nil {
			return err
		}

		t = criurpc.CriuReqType_PRE_DUMP
	} else {
		t = criurpc.CriuReqType_DUMP
	}
	if criuOpts.LazyPages {
		// lazy migration requested; check if criu supports it
		feat := criurpc.CriuFeatures{
			LazyPages: proto.Bool(true),
		}
		if err := c.checkCriuFeatures(criuOpts, &rpcOpts, &feat); err != nil {
			return err
		}

		if fd := criuOpts.StatusFd; fd != -1 {
			// check that the FD is valid
			flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
			if err != nil {
				return fmt.Errorf("invalid --status-fd argument %d: %w", fd, err)
			}
			// and writable
			if flags&unix.O_WRONLY == 0 {
				return fmt.Errorf("invalid --status-fd argument %d: not writable", fd)
			}

			if c.checkCriuVersion(31500) != nil {
				// For criu 3.15+, use notifications (see case "status-ready"
				// in criuNotifications). Otherwise, rely on criu status fd.
				rpcOpts.StatusFd = proto.Int32(int32(fd))
			}
		}
	}

	req := &criurpc.CriuReq{
		Type: &t,
		Opts: &rpcOpts,
	}

	// no need to dump all this in pre-dump
	if !criuOpts.PreDump {
		hasCgroupns := c.Config.Namespaces.Contains(configs.NEWCGROUP)
		for _, m := range c.Config.Mounts {
			switch m.Device {
			case "bind":
				c.addCriuDumpMount(req, m)
			case "cgroup":
				if cgroups.IsCgroup2UnifiedMode() || hasCgroupns {
					// real mount(s)
					continue
				}
				// a set of "external" bind mounts
				binds, err := GetCgroupMounts(m)
				if err != nil {
					return err
				}
				for _, b := range binds {
					c.addCriuDumpMount(req, b)
				}
			}
		}

		if err := c.addMaskPaths(req); err != nil {
			return err
		}

		for _, node := range c.Config.Devices {
			m := &configs.Mount{Destination: node.Path, Source: node.Path}
			c.addCriuDumpMount(req, m)
		}

		// TODO(swarnimarun): was unused, fix in a separate PR after discussion
		// Write the FD info to a file in the image directory
		// fdsJSON, err := json.Marshal(c.InitProcess.externalDescriptors())
		// if err != nil {
		// 	return err
		// }
		// err = os.WriteFile(filepath.Join(criuOpts.ImagesDirectory, descriptorsFilename), fdsJSON, 0o777)
		// if err != nil {
		// 	return err
		// }

		fdsJSON, err := json.Marshal([]string{"/dev/null", "/dev/null", "/dev/null"})
		if err != nil {
			return err
		}

		err = os.WriteFile(filepath.Join(criuOpts.ImagesDirectory, descriptorsFilename), fdsJSON, 0o777)
		if err != nil {
			return err
		}
	}

	err = c.criuSwrk(nil, req, criuOpts, nil)
	if err != nil {
		logCriuErrors(criuOpts.ImagesDirectory, "dump.log")
		return err
	}
	return nil
}

func (c *RuncContainer) criuSwrk(process *Process, req *criurpc.CriuReq, opts *CriuOpts, extraFiles []*os.File) error {
	fds, err := unix.Socketpair(unix.AF_LOCAL, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}

	var logPath string
	if opts != nil {
		logPath = filepath.Join(opts.WorkDirectory, req.GetOpts().GetLogFile())
	} else {
		// For the VERSION RPC 'opts' is set to 'nil' and therefore
		// opts.WorkDirectory does not exist. Set logPath to "".
		logPath = ""
	}
	criuClient := os.NewFile(uintptr(fds[0]), "criu-transport-client")
	criuClientFileCon, err := net.FileConn(criuClient)
	criuClient.Close()
	if err != nil {
		return err
	}

	criuClientCon := criuClientFileCon.(*net.UnixConn)
	defer criuClientCon.Close()

	criuServer := os.NewFile(uintptr(fds[1]), "criu-transport-server")
	defer criuServer.Close()

	if c.CriuVersion != 0 {
		// If the CRIU Version is still '0' then this is probably
		// the initial CRIU run to detect the version. Skip it.
		log.Debug().Msgf("Using CRIU %d", c.CriuVersion)
	}
	cmd := exec.Command("criu", "swrk", "3", "--verbosity=4")

	if process != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
		}

		cmd.Stdin = process.Stdin
		cmd.Stdout = process.Stdout
		cmd.Stderr = process.Stderr
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, criuServer)
	if extraFiles != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, extraFiles...)
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	// we close criuServer so that even if CRIU crashes or unexpectedly exits, runc will not hang.
	criuServer.Close()
	// cmd.Process will be replaced by a restored init.
	criuProcess := cmd.Process

	var criuProcessState *os.ProcessState
	defer func() {
		if criuProcessState == nil {
			criuClientCon.Close()
			_, err := criuProcess.Wait()
			if err != nil {
				log.Warn().Msgf("wait on criuProcess returned %v", err)
			}
		}
	}()

	if err := c.criuApplyCgroups(criuProcess.Pid, req); err != nil {
		return err
	}

	var extFds []string
	if process != nil {
		extFds, err = getPipeFds(criuProcess.Pid)
		if err != nil {
			return err
		}
	}

	log.Debug().Msgf("Using CRIU in %s mode", req.GetType().String())
	// In the case of criurpc.CriuReqType_FEATURE_CHECK req.GetOpts()
	// should be empty. For older CRIU versions it still will be
	// available but empty. criurpc.CriuReqType_VERSION actually
	// has no req.GetOpts().
	if log.Logger.GetLevel() >= zerolog.DebugLevel &&
		!(req.GetType() == criurpc.CriuReqType_FEATURE_CHECK ||
			req.GetType() == criurpc.CriuReqType_VERSION) {

		val := reflect.ValueOf(req.GetOpts())
		v := reflect.Indirect(val)
		for i := 0; i < v.NumField(); i++ {
			st := v.Type()
			name := st.Field(i).Name
			if 'A' <= name[0] && name[0] <= 'Z' {
				value := val.MethodByName("Get" + name).Call([]reflect.Value{})
				log.Debug().Msgf("CRIU option %s with value %v", name, value[0])
			}
		}
	}

	data, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	_, err = criuClientCon.Write(data)
	if err != nil {
		return err
	}

	buf := make([]byte, 10*4096)
	oob := make([]byte, 4096)
	for {
		n, oobn, _, _, err := criuClientCon.ReadMsgUnix(buf, oob)
		if req.Opts != nil && req.Opts.StatusFd != nil {
			// Close status_fd as soon as we got something back from criu,
			// assuming it has consumed (reopened) it by this time.
			// Otherwise it will might be left open forever and whoever
			// is waiting on it will wait forever.
			fd := int(*req.Opts.StatusFd)
			_ = unix.Close(fd)
			req.Opts.StatusFd = nil
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("unexpected EOF")
		}
		if n == len(buf) {
			return errors.New("buffer is too small")
		}

		resp := new(criurpc.CriuResp)
		err = proto.Unmarshal(buf[:n], resp)
		if err != nil {
			return err
		}
		t := resp.GetType()
		if !resp.GetSuccess() {
			return fmt.Errorf("criu failed: type %s errno %d", t, resp.GetCrErrno())
		}

		switch t {
		case criurpc.CriuReqType_FEATURE_CHECK:
			logrus.Debugf("Feature check says: %s", resp)
			criuFeatures = resp.GetFeatures()
		case criurpc.CriuReqType_NOTIFY:
			if err := c.criuNotifications(resp, process, cmd, opts, extFds, oob[:oobn]); err != nil {
				return err
			}
			req = &criurpc.CriuReq{
				Type:          &t,
				NotifySuccess: proto.Bool(true),
			}
			data, err = proto.Marshal(req)
			if err != nil {
				return err
			}
			_, err = criuClientCon.Write(data)
			if err != nil {
				return err
			}
			continue
		case criurpc.CriuReqType_RESTORE:
		case criurpc.CriuReqType_DUMP:
		case criurpc.CriuReqType_PRE_DUMP:
		default:
			return fmt.Errorf("unable to parse the response %s", resp.String())
		}

		break
	}

	_ = criuClientCon.CloseWrite()
	// cmd.Wait() waits cmd.goroutines which are used for proxying file descriptors.
	// Here we want to wait only the CRIU process.
	criuProcessState, err = criuProcess.Wait()
	if err != nil {
		return err
	}

	// Try closing unix sockets
	defer func() {
		_ = unix.Close(fds[0])
		_ = unix.Close(fds[1])
	}()

	// In pre-dump mode CRIU is in a loop and waits for
	// the final DUMP command.
	// The current runc pre-dump approach, however, is
	// start criu in PRE_DUMP once for a single pre-dump
	// and not the whole series of pre-dump, pre-dump, ...m, dump
	// If we got the message CriuReqType_PRE_DUMP it means
	// CRIU was successful and we need to forcefully stop CRIU
	if !criuProcessState.Success() && *req.Type != criurpc.CriuReqType_PRE_DUMP {
		return fmt.Errorf("criu failed: %s\nlog file: %s", criuProcessState.String(), logPath)
	}
	return nil
}
