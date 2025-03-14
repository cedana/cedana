package container

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"github.com/coreos/go-systemd/v22/activation"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/cgroups/manager"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/specconv"
	"github.com/opencontainers/runc/libcontainer/system"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LoadSpecFromBundleForRestore loads the spec from the bundle path, and sets it in the context
func LoadSpecFromBundleForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		details := req.GetDetails().GetRunc()
		bundle := details.GetBundle()
		workingDir := details.GetWorkingDir()

		if !strings.HasPrefix(bundle, "/") {
			bundle = filepath.Join(workingDir, bundle)
			details.Bundle = bundle
		}

		configFile := filepath.Join(bundle, runc.SpecConfigFile)

		spec, err := runc.LoadSpec(configFile)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load spec: %v", err)
		}

		ctx = context.WithValue(ctx, runc_keys.SPEC_CONTEXT_KEY, spec)

		return next(ctx, opts, resp, req)
	}
}

func CreateContainerForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		details := req.GetDetails().GetRunc()
		root := details.GetRoot()
		id := details.GetID()
		bundle := details.GetBundle()

		spec, ok := ctx.Value(runc_keys.SPEC_CONTEXT_KEY).(*specs.Spec)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get spec from context")
		}
		if !strings.HasPrefix(spec.Root.Path, "/") {
			spec.Root.Path = filepath.Join(bundle, spec.Root.Path)
		}

		// TODO SA: get env will not provide the env to the daemon provide env as option
		notifySocket := runc.NewNotifySocket(root, os.Getenv("NOTIFY_SOCKET"), id)
		if notifySocket != nil {
			notifySocket.SetupSpec(spec)
		}

		config, err := specconv.CreateLibcontainerConfig(&specconv.CreateOpts{
			CgroupName:      id,
			Spec:            spec,
			RootlessEUID:    os.Geteuid() != 0,
			RootlessCgroups: false,
		})
		labels := config.Labels
		config.Labels = []string{}
		for _, label := range labels {
			if !strings.HasPrefix(label, "bundle=") {
				config.Labels = append(config.Labels, label)
			}
		}
		config.Labels = append(config.Labels, "bundle="+details.Bundle)

		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create libcontainer config: %v", err)
		}

		container, err := libcontainer.Create(root, id, config)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "failed to create container: %v", err)
		}
		defer func() {
			if err != nil {
				container.Destroy()
			}
		}()

		if notifySocket != nil {
			if err := notifySocket.SetupSocketDirectory(); err != nil {
				return nil, err
			}
		}

		// TODO SA: get env will not provide the env to the daemon provide env as option
		listenFDs := []*os.File{}
		if os.Getenv("LISTEN_FDS") != "" {
			listenFDs = activation.Files(false)
		}

		// XXX: Create new cgroup manager, as the container's cgroup manager is not accessible (internal)
		manager, err := manager.New(config.Cgroups)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create cgroup manager: %v", err)
		}

		// Check that cgroup does not exist or empty (no processes).
		// Note for cgroup v1 this check is not thorough, as there are multiple
		// separate hierarchies, while both Exists() and GetAllPids() only use
		// one for "devices" controller (assuming others are the same, which is
		// probably true in almost all scenarios). Checking all the hierarchies
		// would be too expensive.
		if manager.Exists() {
			pids, err := manager.GetAllPids()
			// Reading PIDs can race with cgroups removal, so ignore ENOENT and ENODEV.
			if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, unix.ENODEV) {
				return nil, status.Errorf(codes.Internal, "failed to get cgroup pids: %v", err)
			}
			if len(pids) != 0 {
				return nil, status.Errorf(codes.FailedPrecondition, "container's cgroup is not empty")
			}
		}

		// Check that cgroup is not frozen. Do not use Exists() here
		// since in cgroup v1 it only checks "devices" controller.
		st, err := manager.GetFreezerState()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get cgroup freezer state: %v", err)
		}
		if st == configs.Frozen {
			return nil, status.Errorf(codes.FailedPrecondition, "container's cgroup unexpectedly frozen")
		}

		process, err := runc.NewProcess(*spec.Process)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create new init process: %v", err)
		}
		process.Init = true
		process.SubCgroupPaths = make(map[string]string)
		if len(listenFDs) > 0 {
			process.Env = append(process.Env, "LISTEN_FDS="+strconv.Itoa(len(listenFDs)), "LISTEN_PID=1")
			process.ExtraFiles = append(process.ExtraFiles, listenFDs...)
		}
		rootuid, err := config.HostRootUID()
		if err != nil {
			return nil, err
		}
		rootgid, err := config.HostRootGID()
		if err != nil {
			return nil, err
		}
		// Setting up IO is a two stage process. We need to modify process to deal
		// with detaching containers, and then we get a tty after the container has
		// started.
		handler := runc.NewSignalHandler(notifySocket)
		tty, err := runc.SetupIO(process, rootuid, rootgid, spec.Process.Terminal, details.Detach, details.ConsoleSocketPath)
		if err != nil {
			return nil, err
		}
		// defer tty.Close()

		ctx = context.WithValue(ctx, runc_keys.CONTAINER_CONTEXT_KEY, container)
		ctx = context.WithValue(ctx, runc_keys.CONTAINER_CGROUP_MANAGER_CONTEXT_KEY, manager)
		ctx = context.WithValue(ctx, runc_keys.INIT_PROCESS_CONTEXT_KEY, process)
		ctx = context.WithValue(ctx, runc_keys.SIGNAL_HANDLER_CONTEXT_KEY, handler)
		ctx = context.WithValue(ctx, runc_keys.TTY_CONTEXT_KEY, tty)

		exited, err = next(ctx, opts, resp, req)
		if err != nil {
			return nil, err
		}

		// Launch a background routine that ensures the container is
		// cleaned up after it exits. Only does so if a valid exit channel is received,
		// ie. when the container managed by the daemon (job).

		if exited == nil { // probably not a managed restore, so we don't care
			return exited, nil
		}

		// TODO SA: handle unmanaged workloads separately
		// if !strings.HasPrefix(details.Root, "/run/containerd/runc/k8s.io") {
		opts.WG.Add(1)
		go func() {
			defer opts.WG.Done()
			<-exited
			log.Debug().Str("id", container.ID()).Msg("runc container exited, cleaning up")
			container.Destroy()
		}()
		return exited, nil
		// }
		// return nil, nil
	}
}

// Adds CRIU callback to run the prestart and create runtime hooks
// before the namespaces are setup during restore
func RunHooksOnRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}
		process, ok := ctx.Value(runc_keys.INIT_PROCESS_CONTEXT_KEY).(*libcontainer.Process)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get process from context")
		}

		config := container.Config()
		callback := &criu.NotifyCallback{
			SetupNamespacesFunc: func(ctx context.Context, pid int32) error {
				if config.Hooks != nil {
					s, err := container.OCIState()
					if err != nil {
						return nil
					}
					s.Pid = int(pid)

					if err := config.Hooks.Run(configs.Prestart, s); err != nil {
						return fmt.Errorf("failed to run prestart hooks: %v", err)
					}
					if err := config.Hooks.Run(configs.CreateRuntime, s); err != nil {
						return fmt.Errorf("failed to run create runtime hooks: %v", err)
					}
				}
				return nil
			},
			OrphanPtsMasterFunc: func(ctx context.Context, fd int32) error {
				log.Debug().Msg("orphan pts master setup console socket")
				master := os.NewFile(uintptr(fd), "orphan-pts-master")
				defer master.Close()

				// While we can access console.master, using the API is a good idea.
				if err := SendFile(process.ConsoleSocket, master); err != nil {
					return err
				}
				return nil
			},
		}
		opts.CRIUCallback.Include(callback)

		return next(ctx, opts, resp, req)
	}
}

const MaxNameLen = 4096

// SendFile sends a file over the given AF_UNIX socket. file.Name() is also
// included so that if the other end uses RecvFile, the file will have the same
// name information.
func SendFile(socket *os.File, file *os.File) error {
	name := file.Name()
	if len(name) >= MaxNameLen {
		return fmt.Errorf("sendfd: filename too long: %s", name)
	}
	err := SendRawFd(socket, name, file.Fd())
	runtime.KeepAlive(file)
	return err
}

// SendRawFd sends a specific file descriptor over the given AF_UNIX socket.
func SendRawFd(socket *os.File, msg string, fd uintptr) error {
	oob := unix.UnixRights(int(fd))
	return unix.Sendmsg(int(socket.Fd()), []byte(msg), oob, nil, 0)
}

// UpdateStateOnRestore updates the container state after restore
// Without this, runc won't be able to 'detect' the container
func UpdateStateOnRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}
		root := req.GetDetails().GetRunc().GetRoot()
		id := req.GetDetails().GetRunc().GetID()

		callback := &criu.NotifyCallback{
			PostRestoreFunc: func(ctx context.Context, pid int32) error {
				state, err := container.State()
				if err != nil {
					return fmt.Errorf("failed to get container state: %v", err)
				}

				// XXX: Unfortunately, 'state' interface is internal to libcontainer
				// but it's simple enough to replicate here, as we only need to update
				// a few fields upon restore, rest should already be set correctly.

				state.Created = time.Now().UTC()
				state.InitProcessPid = int(pid)
				stat, err := system.Stat(int(pid))
				if err != nil {
					log.Warn().Err(err).Msg("failed to get accurate process start time")
				} else {
					state.InitProcessStartTime = stat.StartTime
				}

				for _, ns := range state.Config.Namespaces {
					state.NamespacePaths[ns.Type] = ns.GetPath(int(pid))
				}
				for _, nsType := range configs.NamespaceTypes() {
					if !configs.IsNamespaceSupported(nsType) {
						continue
					}
					if _, ok := state.NamespacePaths[nsType]; !ok {
						ns := configs.Namespace{Type: nsType}
						state.NamespacePaths[ns.Type] = ns.GetPath(int(pid))
					}
				}

				fds, err := GetStdioFds(pid)
				if err != nil {
					return fmt.Errorf("failed to get stdio fds: %v", err)
				} else {
					state.ExternalDescriptors = fds
				}

				err = SaveState(root, id, state)
				if err != nil {
					return fmt.Errorf("failed to save container state: %v", err)
				}
				return nil
			},
		}
		opts.CRIUCallback.Include(callback)

		return next(ctx, opts, resp, req)
	}
}
