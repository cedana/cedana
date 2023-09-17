package container

import (
	"bytes"
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	goruntime "runtime"

	"github.com/cedana/cedana/utils"
	"github.com/checkpoint-restore/go-criu/v5"
	criurpc "github.com/checkpoint-restore/go-criu/v5/rpc"
	"github.com/containerd/console"
	containerd "github.com/containerd/containerd"
	apiTasks "github.com/containerd/containerd/api/services/tasks/v1"
	"github.com/containerd/containerd/api/types"
	containerdTypes "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/archive"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/cmd/ctr/commands/tasks"
	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/diff"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/pkg/epoch"
	"github.com/containerd/containerd/plugin"
	"github.com/containerd/containerd/protobuf"
	ptypes "github.com/containerd/containerd/protobuf/types"
	"github.com/containerd/containerd/rootfs"
	"github.com/containerd/containerd/runtime/linux/runctypes"
	"github.com/containerd/containerd/runtime/v2/runc/options"
	"github.com/containerd/typeurl/v2"
	securejoin "github.com/cyphar/filepath-securejoin"
	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	dockercli "github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/opencontainers/go-digest"
	is "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/cgroups/manager"
	"github.com/opencontainers/runc/libcontainer/configs"
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

const (
	checkpointDateFormat = "01-02-2006-15:04:05"
	checkpointNameFormat = "containerd.io/checkpoint/%s:%s"
)

type RuncContainer struct {
	id                   string
	root                 string
	pid                  int
	config               *configs.Config // standin for configs.Config from runc
	cgroupManager        cgroups.Manager
	initProcessStartTime uint64
	initProcess          parentProcess
	m                    sync.Mutex
	criuVersion          int
	created              time.Time
	dockerConfig         *dockerTypes.ContainerJSON
	intelRdtManager      *Manager
	state                containerState
}

// this comes from runc, see github.com/opencontainers/runc
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
}

type loadedState struct {
	c *RuncContainer
	s Status
}

func (n *loadedState) status() Status {
	return n.s
}

func (n *loadedState) transition(s containerState) error {
	n.c.state = s
	return nil
}

// func (n *loadedState) destroy() error {
// 	if err := n.c.refreshState(); err != nil {
// 		return err
// 	}
// 	return n.c.state.destroy()
// }

type nonChildProcess struct {
	processPid       int
	processStartTime uint64
	fds              []string
}

func getContainerFromRunc(containerID string) *RuncContainer {
	// Runc root
	// root := "/var/run/runc"
	// Docker root
	// root := "/run/docker/runtime-runc/moby"
	// Containerd root where "default" is the namespace
	root := "/run/containerd/runc/default"

	l := utils.GetLogger()

	criu := criu.MakeCriu()
	criuVersion, err := criu.GetCriuVersion()
	if err != nil {
		l.Fatal().Err(err).Msg("could not get criu version")
	}
	root = root + "/" + containerID
	state, err := loadState(root)
	if err != nil {
		l.Fatal().Err(err).Msg("could not load state")
	}

	r := &nonChildProcess{
		processPid:       state.InitProcessPid,
		processStartTime: state.InitProcessStartTime,
		fds:              state.ExternalDescriptors,
	}

	cgroupManager, err := manager.NewWithPaths(state.Config.Cgroups, state.CgroupPaths)
	if err != nil {
		l.Fatal().Err(err).Msg("could not create cgroup manager")
	}

	c := &RuncContainer{
		initProcess:          r,
		initProcessStartTime: state.InitProcessStartTime,
		id:                   containerID,
		root:                 root,
		criuVersion:          criuVersion,
		cgroupManager:        cgroupManager,
		// dockerConfig:  &container,
		config:          &state.Config,
		intelRdtManager: NewManager(&state.Config, containerID, state.IntelRdtPath),
		pid:             state.InitProcessPid,
		// state:           containerState,
		created: state.Created,
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

func newContainerdClient(ctx gocontext.Context, opts ...containerd.ClientOpt) (*containerd.Client, gocontext.Context, gocontext.CancelFunc, error) {
	timeoutOpt := containerd.WithTimeout(0)
	opts = append(opts, timeoutOpt)
	client, err := containerd.New("/run/containerd/containerd.sock", opts...)
	ctx, cancel := AppContext(ctx)
	return client, ctx, cancel, err
}

// Pretty wacky function. "creates" a runc container from a docker container,
// basically piecing it together from information we can parse out from the docker go lib
func getContainerFromDocker(containerID string) *RuncContainer {
	l := utils.GetLogger()

	cli, err := dockercli.NewClientWithOpts(client.FromEnv)
	if err != nil {
		l.Fatal().Err(err).Msg("could not create docker client")
	}

	cli.NegotiateAPIVersion(gocontext.Background())

	container, err := cli.ContainerInspect(gocontext.Background(), containerID)
	if err != nil {
		l.Fatal().Err(err).Msg("could not inspect container")
	}

	criu := criu.MakeCriu()
	criuVersion, err := criu.GetCriuVersion()
	if err != nil {
		l.Fatal().Err(err).Msg("could not get criu version")
	}

	// need to build a config from the information we can parse out from the docker lib
	// start with bare minimum
	runcConf := &configs.Config{
		Rootfs: container.GraphDriver.Data["MergedDir"], // does this work lol
	}

	// create a cgroup manager for cgroup freezing
	// need c.Path, c.Parent & c.Name, c.Systemd. We can grab t his from proc/pid
	var cgroupsConf *configs.Cgroup
	if container.State.Pid != 0 {
		cgroupPaths := []string{fmt.Sprintf("/proc/%d/cgroup", container.State.Pid)}
		// assume we're in cgroup v2 unified
		// for cgroup v2 unified hierarchy, there are no per-controller cgroup paths
		cgroupsPaths, err := cgroups.ParseCgroupFile(cgroupPaths[0])
		if err != nil {
			l.Fatal().Err(err).Msg("could not parse cgroup paths")
		}

		path := cgroupsPaths[""]

		// Splitting the string by / separator
		cgroupParts := strings.Split(path, "/")

		if len(cgroupParts) < 3 {
			l.Fatal().Err(err).Msg("could not parse cgroup path")
		}

		name := cgroupParts[2]
		parent := cgroupParts[1]
		cgpath := "/" + parent + "/" + name

		var isSystemd bool
		if strings.Contains(path, ".slice") {
			isSystemd = true
		}

		cgroupsConf = &configs.Cgroup{
			Parent:  parent,
			Name:    name,
			Path:    cgpath,
			Systemd: isSystemd,
		}

	}

	cgroupManager, err := manager.New(cgroupsConf)
	if err != nil {
		l.Fatal().Err(err).Msg("could not create cgroup manager")
	}

	// this is so stupid hahahaha
	c := &RuncContainer{
		id:            containerID,
		root:          fmt.Sprintf("%s", container.Config.WorkingDir),
		criuVersion:   criuVersion,
		cgroupManager: cgroupManager,
		dockerConfig:  &container,
		config:        runcConf,
		pid:           container.State.Pid,
	}

	return c
}

// Gotta figure out containerID discovery - TODO NR
func Dump(dir string, containerID string) error {
	// create a CriuOpts and pass into RuncCheckpoint
	// opts := &CriuOpts{
	// 	ImagesDirectory: dir,
	// 	LeaveRunning:    true,
	// }
	// Come back to this later. First runc restore
	// c := getContainerFromDocker(containerID)

	dir = "containerd.io/checkpoint/test11:09-16-2023-22:11:37"

	// containerdCheckpoint(containerID, dir)
	containerdRestore(containerID, dir)

	return nil
}

//  CheckpointOpts holds the options for performing a criu checkpoint using runc
// type CheckpointOpts struct {
// 	// ImagePath is the path for saving the criu image file
// 	ImagePath string
// 	// WorkDir is the working directory for criu
// 	WorkDir string
// 	// ParentPath is the path for previous image files from a pre-dump
// 	ParentPath string
// 	// AllowOpenTCP allows open tcp connections to be checkpointed
// 	AllowOpenTCP bool
// 	// AllowExternalUnixSockets allows external unix sockets to be checkpointed
// 	AllowExternalUnixSockets bool
// 	// AllowTerminal allows the terminal(pty) to be checkpointed with a container
// 	AllowTerminal bool
// 	// CriuPageServer is the address:port for the criu page server
// 	CriuPageServer string
// 	// FileLocks handle file locks held by the container
// 	FileLocks bool
// 	// Cgroups is the cgroup mode for how to handle the checkpoint of a container's cgroups
// 	Cgroups CgroupMode
// 	// EmptyNamespaces creates a namespace for the container but does not save its properties
// 	// Provide the namespaces you wish to be checkpointed without their settings on restore
// 	EmptyNamespaces []string
// 	// LazyPages uses userfaultfd to lazily restore memory pages
// 	LazyPages bool
// 	// StatusFile is the file criu writes \0 to once lazy-pages is ready
// 	StatusFile *os.File
// 	ExtraArgs  []string
// }

// AppContext returns the context for a command. Should only be called once per
// command, near the start.
//
// This will ensure the namespace is picked up and set the timeout, if one is
// defined.
func AppContext(context gocontext.Context) (gocontext.Context, gocontext.CancelFunc) {
	var (
		ctx       = gocontext.Background()
		timeout   = 0
		namespace = "default"
		cancel    gocontext.CancelFunc
	)
	ctx = namespaces.WithNamespace(ctx, namespace)
	if timeout > 0 {
		ctx, cancel = gocontext.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	} else {
		ctx, cancel = gocontext.WithCancel(ctx)
	}
	if tm, err := epoch.SourceDateEpoch(); err != nil {
		log.L.WithError(err).Warn("Failed to read SOURCE_DATE_EPOCH")
	} else if tm != nil {
		log.L.Debugf("Using SOURCE_DATE_EPOCH: %v", tm)
		ctx = epoch.WithSourceDateEpoch(ctx, tm)
	}
	return ctx, cancel
}

func containerdRestore(id string, ref string) error {
	ctx := gocontext.Background()
	containerdClient, ctx, cancel, err := newContainerdClient(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	checkpoint, err := containerdClient.GetImage(ctx, ref)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}
		// TODO (ehazlett): consider other options (always/never fetch)
		ck, err := containerdClient.Fetch(ctx, ref)
		if err != nil {
			return err
		}
		checkpoint = containerd.NewImage(containerdClient, ck)
	}

	opts := []containerd.RestoreOpts{
		containerd.WithRestoreImage,
		containerd.WithRestoreSpec,
		containerd.WithRestoreRuntime,
	}
	// if context.Bool("rw") {
	// 	opts = append(opts, containerd.WithRestoreRW)
	// }

	ctr, err := containerdClient.Restore(ctx, id, checkpoint, opts...)
	if err != nil {
		return err
	}
	topts := []containerd.NewTaskOpts{}
	// if context.Bool("live") {
	// 	topts = append(topts, containerd.WithTaskCheckpoint(checkpoint))
	// }
	spec, err := ctr.Spec(ctx)
	if err != nil {
		return err
	}

	useTTY := spec.Process.Terminal

	var con console.Console
	if useTTY {
		con = console.Current()
		defer con.Reset()
		if err := con.SetRaw(); err != nil {
			return err
		}
	}

	task, err := tasks.NewTask(ctx, containerdClient, ctr, "", con, false, "", []cio.Opt{}, topts...)
	if err != nil {
		return err
	}

	var statusC <-chan containerd.ExitStatus
	if useTTY {
		if statusC, err = task.Wait(ctx); err != nil {
			return err
		}
	}

	if err := task.Start(ctx); err != nil {
		return err
	}
	if !useTTY {
		return nil
	}

	if err := tasks.HandleConsoleResize(ctx, task, con); err != nil {
		log.G(ctx).WithError(err).Error("console resize")
	}

	status := <-statusC
	code, _, err := status.Result()
	if err != nil {
		return err
	}
	if _, err := task.Delete(ctx); err != nil {
		return err
	}
	if code != 0 {
		return errors.New("exit code not 0")
	}
	return nil
}

func containerdCheckpoint(id string, ref string) error {

	ctx := gocontext.Background()

	containerdClient, ctx, cancel, err := newContainerdClient(ctx)
	if err != nil {
		logrus.Fatal(err)
	}
	defer cancel()

	// containerdOpts := []containerd.CheckpointOpts{
	// 	containerd.WithCheckpointRuntime,
	// }

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

	// info, err := container.Info(ctx)
	if err != nil {
		return err
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

	// opts := []containerd.CheckpointTaskOpts{withCheckpointOpts(info.Runtime.Name, ctx)}

	// Original task checkpoint

	// newOpts := []CheckpointTaskOpts{
	// 	func(r *CheckpointTaskInfo) error {
	// 		imagePath := "$HOME/.cedana/dumpdir"
	// 		workPath := ""

	// 		if r.Options == nil {
	// 			r.Options = &options.CheckpointOptions{}
	// 		}
	// 		opts, _ := r.Options.(*options.CheckpointOptions)

	// 		// if context.Bool("exit") {
	// 		opts.Exit = false
	// 		// }
	// 		if imagePath != "" {
	// 			opts.ImagePath = imagePath
	// 		}
	// 		if workPath != "" {
	// 			opts.WorkPath = workPath
	// 		}

	// 		return nil
	// 	},
	// }

	// create image path store criu image files
	imagePath := ""
	// checkpoint task
	// if _, err := task.Checkpoint(ctx, containerd.WithCheckpointImagePath(imagePath)); err != nil {
	// 	return err
	// }
	checkpoint, err := runcCheckpointContainerd(ctx, containerdClient, task, WithCheckpointImagePath(imagePath))

	if err != nil {
		return err
	}

	fmt.Println(checkpoint.Name())
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

func localCheckpointTask(ctx gocontext.Context, client *containerd.Client, index *v1.Index, request *apiTasks.CheckpointTaskRequest, container containers.Container) (*apiTasks.CheckpointTaskResponse, error) {

	v, err := typeurl.UnmarshalAny(request.Options)
	if err != nil {
		return &apiTasks.CheckpointTaskResponse{}, err
	}
	opts, ok := v.(*options.CheckpointOptions)
	if !ok {
		return &apiTasks.CheckpointTaskResponse{}, fmt.Errorf("invalid task checkpoint option for %s", container.Runtime.Name)
	}

	criuOpts := &CriuOpts{
		ImagesDirectory:         opts.ImagePath,
		WorkDirectory:           opts.WorkPath,
		LeaveRunning:            !opts.Exit,
		TcpEstablished:          opts.OpenTcp,
		ExternalUnixConnections: opts.ExternalUnixSockets,
		ShellJob:                opts.Terminal,
		FileLocks:               opts.FileLocks,
		StatusFd:                int(3),
	}

	image := opts.ImagePath

	checkpointImageExists := false

	if image == "" {
		checkpointImageExists = true
		image, err = os.MkdirTemp(os.Getenv("XDG_RUNTIME_DIR"), "ctrd-checkpoint")
		if err != nil {
			return &apiTasks.CheckpointTaskResponse{}, err
		}
		criuOpts.ImagesDirectory = image
		defer os.RemoveAll(image)
	}

	// Replace with our criu checkpoint
	// if err := runtimeTask.Checkpoint(ctx, image, request.Options); err != nil {
	// 	return &apiTasks.CheckpointTaskResponse{}, err
	// }

	c := getContainerFromRunc(container.ID)

	err = c.RuncCheckpoint(criuOpts, c.pid)
	if err != nil {
		return nil, err
	}

	// do not commit checkpoint image if checkpoint ImagePath is passed,
	// return if checkpointImageExists is false
	if !checkpointImageExists {
		return &apiTasks.CheckpointTaskResponse{}, nil
	}
	// write checkpoint to the content store
	tar := archive.Diff(ctx, "", image)
	cp, err := localWriteContent(ctx, client, images.MediaTypeContainerd1Checkpoint, image, tar)
	if err != nil {
		return nil, err
	}
	// close tar first after write
	if err := tar.Close(); err != nil {
		return &apiTasks.CheckpointTaskResponse{}, err
	}
	if err != nil {
		return &apiTasks.CheckpointTaskResponse{}, err
	}
	// write the config to the content store
	pbany := protobuf.FromAny(container.Spec)
	data, err := proto.Marshal(pbany)
	if err != nil {
		return &apiTasks.CheckpointTaskResponse{}, err
	}
	spec := bytes.NewReader(data)
	specD, err := localWriteContent(ctx, client, images.MediaTypeContainerd1CheckpointConfig, filepath.Join(image, "spec"), spec)
	if err != nil {
		return &apiTasks.CheckpointTaskResponse{}, err
	}
	return &apiTasks.CheckpointTaskResponse{
		Descriptors: []*containerdTypes.Descriptor{
			cp,
			specD,
		},
	}, nil

}

func localWriteContent(ctx gocontext.Context, client *containerd.Client, mediaType, ref string, r io.Reader) (*types.Descriptor, error) {
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

// func criuCheckpoint(ctx context.Context, containerId string, ctr *apiTasksV2.CheckpointTaskRequest) error {

// 	var opts options.CheckpointOptions
// 	if ctr.Options != nil {
// 		if err := typeurl.UnmarshalTo(ctr.Options, &opts); err != nil {
// 			return err
// 		}
// 	}

// 	r := &process.CheckpointConfig{
// 		Path:                     ctr.Path,
// 		Exit:                     opts.Exit,
// 		AllowOpenTCP:             opts.OpenTcp,
// 		AllowExternalUnixSockets: opts.ExternalUnixSockets,
// 		AllowTerminal:            opts.Terminal,
// 		FileLocks:                opts.FileLocks,
// 		EmptyNamespaces:          opts.EmptyNamespaces,
// 		WorkDir:                  opts.WorkPath,
// 	}

// 	var actions []runc.CheckpointAction
// 	if !r.Exit {
// 		actions = append(actions, runc.LeaveRunning)
// 	}
// 	// keep criu work directory if criu work dir is set
// 	work := r.WorkDir
// 	if work == "" {
// 		work = filepath.Join("", "criu-work")
// 		defer os.RemoveAll(work)
// 	}
// 	if err := actualCriuCheckpoint(ctx, containerId, &runc.CheckpointOpts{
// 		WorkDir:                  work,
// 		ImagePath:                r.Path,
// 		AllowOpenTCP:             r.AllowOpenTCP,
// 		AllowExternalUnixSockets: r.AllowExternalUnixSockets,
// 		AllowTerminal:            r.AllowTerminal,
// 		FileLocks:                r.FileLocks,
// 		EmptyNamespaces:          r.EmptyNamespaces,
// 	}, actions...); err != nil {
// 		Bundle := ""
// 		dumpLog := filepath.Join(Bundle, "criu-dump.log")
// 		if cerr := copyFile(dumpLog, filepath.Join(work, "dump.log")); cerr != nil {
// 			log.G(ctx).WithError(cerr).Error("failed to copy dump.log to criu-dump.log")
// 		}
// 		return fmt.Errorf("%s path= %s", err, dumpLog)
// 	}
// 	return nil
// }

// func actualCriuCheckpoint(context context.Context, id string, opts *runc.CheckpointOpts, actions ...runc.CheckpointAction) error {
// 	args := []string{"checkpoint"}
// 	extraFiles := []*os.File{}
// 	if opts != nil {
// 		args = append(args, opts.args()...)
// 		if opts.StatusFile != nil {
// 			// pass the status file to the child process
// 			extraFiles = []*os.File{opts.StatusFile}
// 			// set status-fd to 3 as this will be the file descriptor
// 			// of the first file passed with cmd.ExtraFiles
// 			args = append(args, "--status-fd", "3")
// 		}
// 	}
// 	for _, a := range actions {
// 		args = a(args)
// 	}
// 	cmd := r.command(context, append(args, id)...)
// 	cmd.ExtraFiles = extraFiles
// 	return r.runOrError(cmd)
// }

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

// Runtime name for the container
func (i *CheckpointTaskInfo) Runtime() string {
	return i.runtime
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

func runcCheckpointContainerd(ctx gocontext.Context, client *containerd.Client, task containerd.Task, opts ...CheckpointTaskOpts) (containerd.Image, error) {
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
				OS:           goruntime.GOOS,
				Architecture: goruntime.GOARCH,
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

func writeIndex(ctx gocontext.Context, client *containerd.Client, task containerd.Task, index *v1.Index) (d v1.Descriptor, err error) {
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

func writeContent(ctx gocontext.Context, store content.Ingester, mediaType, ref string, r io.Reader, opts ...content.Opt) (d v1.Descriptor, err error) {
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

func checkpointImage(ctx gocontext.Context, client *containerd.Client, index *v1.Index, image string) error {
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

func checkpointRWSnapshot(ctx gocontext.Context, client *containerd.Client, index *v1.Index, snapshotterName string, id string) error {
	opts := []diff.Opt{
		diff.WithReference(fmt.Sprintf("checkpoint-rw-%s", id)),
	}
	rw, err := rootfs.CreateDiff(ctx, id, client.SnapshotService(snapshotterName), client.DiffService(), opts...)
	if err != nil {
		return err
	}
	rw.Platform = &v1.Platform{
		OS:           goruntime.GOOS,
		Architecture: goruntime.GOARCH,
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

func (c *RuncContainer) RuncCheckpoint(criuOpts *CriuOpts, pid int) error {
	c.m.Lock()
	defer c.m.Unlock()

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
	if err := os.Mkdir(criuOpts.ImagesDirectory, 0o700); err != nil && !os.IsExist(err) {
		return err
	}

	imageDir, err := os.Open(criuOpts.ImagesDirectory)
	if err != nil {
		return err
	}
	defer imageDir.Close()

	rpcOpts := criurpc.CriuOpts{
		ImagesDirFd:     proto.Int32(int32(imageDir.Fd())),
		LogLevel:        proto.Int32(4),
		LogFile:         proto.String("dump.log"),
		Root:            proto.String(c.config.Rootfs), // TODO NR:not sure if workingDir is analogous here
		ManageCgroups:   proto.Bool(true),
		NotifyScripts:   proto.Bool(false),
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
	}

	// if criuOpts.WorkDirectory is not set, criu default is used.
	if criuOpts.WorkDirectory != "" {
		if err := os.Mkdir(criuOpts.WorkDirectory, 0o700); err != nil && !os.IsExist(err) {
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
		if fcg := c.cgroupManager.Path("freezer"); fcg != "" {
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

	req := &criurpc.CriuReq{
		Type: &t,
		Opts: &rpcOpts,
	}

	err = c.criuSwrk(nil, req, criuOpts, nil)
	if err != nil {
		return err
	}
	return nil
}

func (c *RuncContainer) criuSwrk(process *libcontainer.Process, req *criurpc.CriuReq, opts *CriuOpts, extraFiles []*os.File) error {
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

	if c.criuVersion != 0 {
		// If the CRIU Version is still '0' then this is probably
		// the initial CRIU run to detect the version. Skip it.
		logrus.Debugf("Using CRIU %d", c.criuVersion)
	}
	cmd := exec.Command("criu", "swrk", "3")
	if process != nil {
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
				logrus.Warnf("wait on criuProcess returned %v", err)
			}
		}
	}()

	if err := c.criuApplyCgroups(criuProcess.Pid, req); err != nil {
		return err
	}

	logrus.Debugf("Using CRIU in %s mode", req.GetType().String())
	// In the case of criurpc.CriuReqType_FEATURE_CHECK req.GetOpts()
	// should be empty. For older CRIU versions it still will be
	// available but empty. criurpc.CriuReqType_VERSION actually
	// has no req.GetOpts().
	if logrus.GetLevel() >= logrus.DebugLevel &&
		!(req.GetType() == criurpc.CriuReqType_FEATURE_CHECK ||
			req.GetType() == criurpc.CriuReqType_VERSION) {

		val := reflect.ValueOf(req.GetOpts())
		v := reflect.Indirect(val)
		for i := 0; i < v.NumField(); i++ {
			st := v.Type()
			name := st.Field(i).Name
			if 'A' <= name[0] && name[0] <= 'Z' {
				value := val.MethodByName("Get" + name).Call([]reflect.Value{})
				logrus.Debugf("CRIU option %s with value %v", name, value[0])
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
		n, _, _, _, err := criuClientCon.ReadMsgUnix(buf, oob)
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
		if !resp.GetSuccess() {
			typeString := req.GetType().String()
			return fmt.Errorf("criu failed: type %s errno %d\nlog file: %s", typeString, resp.GetCrErrno(), logPath)
		}

		t := resp.GetType()
		switch {
		case t == criurpc.CriuReqType_FEATURE_CHECK:
			logrus.Debugf("Feature check says: %s", resp)
			criuFeatures = resp.GetFeatures()
		case t == criurpc.CriuReqType_NOTIFY:
			// removed notify functionality
		case t == criurpc.CriuReqType_RESTORE:
		case t == criurpc.CriuReqType_DUMP:
		case t == criurpc.CriuReqType_PRE_DUMP:
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
