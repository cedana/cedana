package api

// Internal functions used by service for restoring processes and containers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/api/services/gpu"
	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
	"github.com/checkpoint-restore/go-criu/v6/rpc"
	"github.com/containerd/containerd/identifiers"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/typeurl/v2"
	"github.com/shirou/gopsutil/v3/process"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	"github.com/opencontainers/runtime-spec/specs-go"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	CRIU_RESTORE_LOG_FILE   = "cedana-restore.log"
	CRIU_RESTORE_LOG_LEVEL  = 4
	RESTORE_TEMPDIR         = "/tmp/cedana_restore"
	RESTORE_TEMPDIR_PERMS   = 0o755
	RESTORE_OUTPUT_LOG_PATH = "/var/log/cedana-output-%s.log"
	KATA_RESTORE_OUTPUT_LOG_PATH = "/tmp/cedana-output-%s.log"
)

func (s *service) prepareRestore(ctx context.Context, opts *rpc.CriuOpts, args *task.RestoreArgs, isKata bool) (*string, *task.ProcessState, []*os.File, error) {
	var isShellJob bool
	var inheritFds []*rpc.InheritFd
	var tcpEstablished bool
	var extraFiles []*os.File

	_, prepareRestoreSpan := s.tracer.Start(ctx, "prepare_restore")
	defer prepareRestoreSpan.End()

	tempDir := RESTORE_TEMPDIR

	// check if tmpdir exists
	// XXX YA: Tempdir usage is not thread safe
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		err := os.Mkdir(tempDir, RESTORE_TEMPDIR_PERMS)
		if err != nil {
			return nil, nil, nil, err
		}
	} else {
		// likely an old checkpoint hanging around, delete
		err := os.RemoveAll(tempDir)
		if err != nil {
			return nil, nil, nil, err
		}
		err = os.Mkdir(tempDir, RESTORE_TEMPDIR_PERMS)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	err := utils.UntarFolder(args.CheckpointPath, tempDir)
	if err != nil {
		s.logger.Error().Err(err).Msg("error decompressing checkpoint")
		return nil, nil, nil, err
	}

	checkpointState, err := deserializeStateFromDir(tempDir)
	if err != nil {
		s.logger.Error().Err(err).Msg("error unmarshaling checkpoint state")
		return nil, nil, nil, err
	}

	if !isKata {
		open_fds := checkpointState.ProcessInfo.OpenFds

		// create logfile for redirection
		filename := fmt.Sprintf(RESTORE_OUTPUT_LOG_PATH, fmt.Sprint(time.Now().Unix()))
		file, err := os.Create(filename)
		if err != nil {
			s.logger.Error().Err(err).Msg("error creating logfile")
			return nil, nil, nil, err
		}

		for _, f := range open_fds {
			if strings.Contains(f.Path, "pts") {
				isShellJob = true
			}
			// if stdout or stderr, always redirect fds
			if f.Fd == 1 || f.Fd == 2 {
				// strip leading slash from f
				f.Path = strings.TrimPrefix(f.Path, "/")

				extraFiles = append(extraFiles, file)
				inheritFds = append(inheritFds, &rpc.InheritFd{
					Fd:  proto.Int32(2 + int32(len(extraFiles))),
					Key: proto.String(f.Path),
				})
			}
		}
	}

	// TODO NR - wtf?
	for _, oc := range checkpointState.ProcessInfo.OpenConnections {
		if oc.Type == syscall.SOCK_STREAM { // TCP
			tcpEstablished = true
		}
	}

	opts.ShellJob = proto.Bool(isShellJob)
	opts.InheritFd = inheritFds
	opts.TcpEstablished = proto.Bool(tcpEstablished || args.TcpEstablished)

	if err := chmodRecursive(tempDir, RESTORE_TEMPDIR_PERMS); err != nil {
		s.logger.Error().Err(err).Msg("error changing permissions")
		return nil, nil, nil, err
	}

	return &tempDir, checkpointState, extraFiles, nil
}

// chmodRecursive changes the permissions of the given path and all its contents.
func chmodRecursive(path string, mode os.FileMode) error {
	return filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chmod(filePath, mode)
	})
}

func (s *service) containerdRestore(ctx context.Context, imgPath string, containerId string) error {
	s.logger.Info().Msgf("restoring container %s from %s", containerId, imgPath)
	err := container.Restore(imgPath, containerId)
	if err != nil {
		return err
	}
	return nil
}

func (s *service) prepareRestoreOpts() *rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel: proto.Int32(CRIU_RESTORE_LOG_LEVEL),
		LogFile:  proto.String(CRIU_RESTORE_LOG_FILE),
	}

	return &opts
}

func (s *service) criuRestore(ctx context.Context, opts *rpc.CriuOpts, nfy Notify, dir string, extraFiles []*os.File) (*int32, error) {
	_, restoreSpan := s.tracer.Start(ctx, "restore")
	restoreSpan.SetAttributes(attribute.Bool("container", false))
	defer restoreSpan.End()

	img, err := os.Open(dir)
	if err != nil {
		s.logger.Fatal().Err(err).Msg("could not open directory")
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))

	resp, err := s.CRIU.Restore(opts, &nfy, extraFiles)
	if err != nil {
		// cleanup along the way
		os.RemoveAll(dir)
		s.logger.Warn().Msgf("error restoring process: %v", err)
		restoreSpan.RecordError(err)
		return nil, err
	}

	s.logger.Info().Int32("PID", *resp.Restore.Pid).Msgf("process restored")

	return resp.Restore.Pid, nil
}

func patchPodmanRestore(ctx context.Context, opts *container.RuncOpts, containerId, imgPath string) error {
	// Podman run -d state
	if !opts.Detach {
		jsonData, err := os.ReadFile(opts.Bundle + "config.json")
		if err != nil {
			return err
		}

		var data map[string]interface{}

		if err := json.Unmarshal(jsonData, &data); err != nil {
			return err
		}

		data["process"].(map[string]interface{})["terminal"] = false
		updatedJSON, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(opts.Bundle+"config.json", updatedJSON, 0644); err != nil {
			return err
		}
	}

	// Here lie the podman patch! :brandon-pirate:
	if err := utils.CRImportCheckpoint(ctx, imgPath, containerId); err != nil {
		return err
	}

	return nil
}

func recursivelyReplace(data interface{}, oldValue, newValue string) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if str, isString := value.(string); isString {
				v[key] = strings.Replace(str, oldValue, newValue, -1)
			} else {
				recursivelyReplace(value, oldValue, newValue)
			}
		}
	case []interface{}:
		for _, value := range v {
			recursivelyReplace(value, oldValue, newValue)
		}
	}
}

type linkPairs struct {
	Key   string
	Value string
}

func (s *service) runcRestore(ctx context.Context, imgPath, containerId string, isK3s bool, sources []string, opts *container.RuncOpts) error {
	ctx, restoreSpan := s.tracer.Start(ctx, "restore")
	restoreSpan.SetAttributes(attribute.Bool("container", true))
	defer restoreSpan.End()

	bundle := Bundle{Bundle: opts.Bundle}

	isPodman := checkIfPodman(bundle)

	if isPodman {
		var spec rspec.Spec
		parts := strings.Split(opts.Bundle, "/")
		oldContainerId := parts[6]

		runPath := "/run/containers/storage/overlay-containers/" + oldContainerId + "/userdata"
		newRunPath := "/run/containers/storage/overlay-containers/" + containerId
		newVarPath := "/var/lib/containers/storage/overlay/" + containerId + "/merged"

		parts[6] = containerId
		// exclude last part for rsync
		parts = parts[1 : len(parts)-1]
		newBundle := "/" + strings.Join(parts, "/")

		if err := rsyncDirectories(opts.Bundle, newBundle); err != nil {
			return err
		}
		if err := rsyncDirectories(runPath, newRunPath); err != nil {
			return err
		}
		configFile, err := os.ReadFile(filepath.Join(newBundle+"/userdata", "config.json"))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(configFile, &spec); err != nil {
			return err
		}
		recursivelyReplace(&spec, oldContainerId, containerId)
		varPath := spec.Root.Path + "/"
		if err := os.Mkdir("/var/lib/containers/storage/overlay/"+containerId, 0644); err != nil {
			return err
		}
		if err := rsyncDirectories(varPath, newVarPath); err != nil {
			return err
		}
		spec.Root.Path = newVarPath
		updatedConfig, err := json.Marshal(spec)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(newBundle+"/userdata", "config.json"), updatedConfig, 0644); err != nil {
			return err
		}
		opts.Bundle = newBundle + "/userdata"
	}

	if isK3s {
		var spec rspec.Spec

		links := []linkPairs{
			{"/host/var/run/netns", "/var/run/netns"},
			{"/host/run/containerd", "/run/containerd"},
			{"/host/var/run/secrets", "/var/run/secrets"},
			{"/host/var/lib/rancher", "/var/lib/rancher"},
			{"/host/run/k3s", "/run/k3s"},
			{"/host/var/lib/kubelet", "/var/lib/kubelet"},
		}
		// Create sym links so that runc c/r can resolve config.json paths to the mounted ones in /host
		for _, link := range links {
			// Check if the target file exists
			if _, err := os.Stat(link.Value); os.IsNotExist(err) {
				// Target file does not exist, attempt to create a symbolic link
				if err := os.Symlink(link.Key, link.Value); err != nil {
					// Handle the error if creating symlink fails
					fmt.Println("Error creating symlink:", err)
					// Handle the error or log it as needed
				}
			} else if err != nil {
				// Handle other errors from os.Stat if any
				fmt.Println("Error checking file info:", err)
				// Handle the error or log it as needed
			}
		}
		// ctx := namespaces.WithNamespace(context.Background(), "k8s.io")
		// parts := strings.Split(opts.Bundle, "/")
		// oldContainerID := parts[7]
		configPath := opts.Bundle + "/config.json" // Replace with your actual path

		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Println("Error reading config.json:", err)
			return err
		}

		// Unmarshal JSON data into a map
		if err := json.Unmarshal(data, &spec); err != nil {
			fmt.Println("Error decoding config.json:", err)
			return err
		}
	}

	err := container.RuncRestore(imgPath, containerId, *opts)
	if err != nil {
		return err
	}

	if isPodman {
		go func() {
			if err := patchPodmanRestore(ctx, opts, containerId, imgPath); err != nil {
				log.Fatal(err)
			}
		}()
	}
	return err
}

// Bundle represents an OCI bundle
type OCIBundle struct {
	// ID of the bundle
	ID string
	// Path to the bundle
	Path string
	// Namespace of the bundle
	Namespace string
}

// ociSpecUserNS is a subset of specs.Spec used to reduce garbage during
// unmarshal.
type ociSpecUserNS struct {
	Linux *linuxSpecUserNS
}

// linuxSpecUserNS is a subset of specs.Linux used to reduce garbage during
// unmarshal.
type linuxSpecUserNS struct {
	GIDMappings []specs.LinuxIDMapping
}

// remappedGID reads the remapped GID 0 from the OCI spec, if it exists. If
// there is no remapping, remappedGID returns 0. If the spec cannot be parsed,
// remappedGID returns an error.
func remappedGID(spec []byte) (uint32, error) {
	var ociSpec ociSpecUserNS
	err := json.Unmarshal(spec, &ociSpec)
	if err != nil {
		return 0, err
	}
	if ociSpec.Linux == nil || len(ociSpec.Linux.GIDMappings) == 0 {
		return 0, nil
	}
	for _, mapping := range ociSpec.Linux.GIDMappings {
		if mapping.ContainerID == 0 {
			return mapping.HostID, nil
		}
	}
	return 0, nil
}

// prepareBundleDirectoryPermissions prepares the permissions of the bundle
// directory according to the needs of the current platform.
// On Linux when user namespaces are enabled, the permissions are modified to
// allow the remapped root GID to access the bundle.
func prepareBundleDirectoryPermissions(path string, spec []byte) error {
	gid, err := remappedGID(spec)
	if err != nil {
		return err
	}
	if gid == 0 {
		return nil
	}
	if err := os.Chown(path, -1, int(gid)); err != nil {
		return err
	}
	return os.Chmod(path, 0710)
}

// NewBundle returns a new bundle on disk
func NewBundle(ctx context.Context, root, state, id string, spec typeurl.Any) (b *OCIBundle, err error) {
	if err := identifiers.Validate(id); err != nil {
		return nil, fmt.Errorf("invalid task id %s: %w", id, err)
	}

	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}
	work := filepath.Join(root, ns, id)
	b = &OCIBundle{
		ID:        id,
		Path:      filepath.Join(state, ns, id),
		Namespace: ns,
	}
	var paths []string
	defer func() {
		if err != nil {
			for _, d := range paths {
				os.RemoveAll(d)
			}
		}
	}()
	// create state directory for the bundle
	if err := os.MkdirAll(filepath.Dir(b.Path), 0711); err != nil {
		return nil, err
	}
	if err := os.Mkdir(b.Path, 0700); err != nil {
		return nil, err
	}
	if typeurl.Is(spec, &specs.Spec{}) {
		if err := prepareBundleDirectoryPermissions(b.Path, spec.GetValue()); err != nil {
			return nil, err
		}
	}
	paths = append(paths, b.Path)
	// create working directory for the bundle
	if err := os.MkdirAll(filepath.Dir(work), 0711); err != nil {
		return nil, err
	}
	rootfs := filepath.Join(b.Path, "rootfs")
	if err := os.MkdirAll(rootfs, 0711); err != nil {
		return nil, err
	}
	paths = append(paths, rootfs)
	if err := os.Mkdir(work, 0711); err != nil {
		if !os.IsExist(err) {
			return nil, err
		}
		os.RemoveAll(work)
		if err := os.Mkdir(work, 0711); err != nil {
			return nil, err
		}
	}
	paths = append(paths, work)
	// symlink workdir
	if err := os.Symlink(work, filepath.Join(b.Path, "work")); err != nil {
		return nil, err
	}
	if spec := spec.GetValue(); spec != nil {
		// write the spec to the bundle
		specPath := filepath.Join(b.Path, "config.json")
		err = os.WriteFile(specPath, spec, 0666)
		if err != nil {
			return nil, fmt.Errorf("failed to write bundle spec: %w", err)
		}
	}
	return b, nil
}

// Define the rsync command and arguments
// Set the output and error streams to os.Stdout and os.Stderr to see the output of rsync
// Run the rsync command

// Using rsync instead of cp -r, for some reason cp -r was not copying all the files and directories over but rsync does...
func rsyncDirectories(source, destination string) error {
	cmd := exec.Command("sudo", "rsync", "-av", "--exclude=attach", source, destination)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func (s *service) restore(ctx context.Context, args *task.RestoreArgs) (*int32, error) {
	var dir *string
	var pid *int32

	opts := s.prepareRestoreOpts()
	nfy := Notify{
		Logger: s.logger,
	}

	dir, state, extraFiles, err := s.prepareRestore(ctx, opts, args, false)
	if err != nil {
		return nil, err
	}

	var gpuCmd *exec.Cmd
	// No GPU flag passed in args - if state.GPUCheckpointed = true, always restore using gpu-controller
	if state.GPUCheckpointed {
		nfy.PreResumeFunc = NotifyFunc{
			Avail: true,
			Callback: func() error {
				var err error
				gpuCmd, err = s.gpuRestore(ctx, *dir, args.UID, args.GID, args.Groups)
				return err
			},
		}
	}

	var gpuerrbuf bytes.Buffer
	if gpuCmd != nil {
		gpuCmd.Stderr = io.Writer(&gpuerrbuf)
	}

	pid, err = s.criuRestore(ctx, opts, nfy, *dir, extraFiles)
	if err != nil {
		return nil, err
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		proc, err := process.NewProcessWithContext(ctx, *pid)
		if err != nil {
			s.logger.Error().Msgf("could not find process: %v", err)
			return
		}
		for {
			running, err := proc.IsRunning()
			if err != nil || !running {
				break
			}
			time.Sleep(1 * time.Second)
		}

		if gpuCmd != nil {
			err = gpuCmd.Process.Kill()
			if err != nil {
				s.logger.Fatal().Err(err)
			}
			s.logger.Info().Int("PID", gpuCmd.Process.Pid).Msgf("GPU controller killed")
			// read last bit of data from /tmp/cedana-gpucontroller.log and print
			s.logger.Debug().Str("Log", gpuerrbuf.String()).Msgf("GPU controller log")
		}
		s.logger.Info().Err(err).Int32("PID", *pid).Msg("process terminated")

		// Update state if it's a managed restored job
		if args.JID != "" {
			childCtx := context.WithoutCancel(ctx) // since this routine can outlive the parent
			state, err := s.getState(childCtx, args.JID)
			if err != nil {
				s.logger.Warn().Err(err).Msg("failed to get state after job done")
				return
			}
			state.JobState = task.JobState_JOB_DONE
			state.PID = *pid
			err = s.updateState(childCtx, args.JID, state)
			if err != nil {
				s.logger.Warn().Err(err).Msg("failed to update state after job done")
			}
		}
	}()

	// If it's a managed restored job, kill on server shutdown
	if args.JID != "" {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			<-s.serverCtx.Done()
			s.logger.Debug().Int32("PID", *pid).Str("JID", args.JID).Msgf("server shutting down, killing process")
			proc, err := process.NewProcessWithContext(ctx, *pid)
			if err != nil {
				s.logger.Warn().Err(err).Msgf("could not find process to kill")
				return
			}
			proc.Kill()
		}()
	}

	return pid, nil
}

func (s *service) kataRestore(ctx context.Context, args *task.RestoreArgs) (*int32, error) {
	var dir *string
	var pid *int32

	opts := s.prepareRestoreOpts()
	nfy := Notify{
		Logger: s.logger,
	}

	dir, state, extraFiles, err := s.prepareRestore(ctx, opts, args, true)
	if err != nil {
		return nil, err
	}

	opts.External = append(opts.External, fmt.Sprintf("mnt[]:m"))
	opts.Root = proto.String("/run/kata-containers/shared/containers/" + args.CheckpointID + "/rootfs")
	opts.InheritFd = nil

	open_fds := state.ProcessInfo.OpenFds

	// create logfile for redirection
	filename := fmt.Sprintf(KATA_RESTORE_OUTPUT_LOG_PATH, fmt.Sprint(time.Now().Unix()))
	file, err := os.Create(filename)
	if err != nil {
		s.logger.Error().Err(err).Msg("error creating logfile")
		return nil, err
	}

	for _, f := range open_fds {
		if f.Fd == 0 || f.Fd == 1 || f.Fd == 2 {
			// strip leading slash from f
			f.Path = strings.TrimPrefix(f.Path, "/")

			extraFiles = append(extraFiles, file)
			opts.InheritFd = append(opts.InheritFd, &rpc.InheritFd{
				Fd:  proto.Int32(2 + int32(len(extraFiles))),
				Key: proto.String(f.Path),
			})
		}
	}

	fmt.Println(opts)

	pid, err = s.criuRestore(ctx, opts, nfy, *dir, extraFiles)
	if err != nil {
		return nil, err
	}

	return pid, nil
}

func (s *service) gpuRestore(ctx context.Context, dir string, uid, gid uint32, groups []uint32) (*exec.Cmd, error) {
	ctx, gpuSpan := s.tracer.Start(ctx, "gpu-restore")
	defer gpuSpan.End()

	gpuCmd, err := s.StartGPUController(ctx, uid, gid, groups, s.logger)
	if err != nil {
		s.logger.Warn().Msgf("could not start cedana-gpu-controller: %v", err)
		return nil, err
	}

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	gpuConn, err := grpc.Dial("127.0.0.1:50051", opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	defer gpuConn.Close()

	gpuServiceConn := gpu.NewCedanaGPUClient(gpuConn)

	args := gpu.RestoreRequest{
		Directory: dir,
	}
	resp, err := gpuServiceConn.Restore(ctx, &args)
	if err != nil {
		s.logger.Warn().Msgf("could not restore gpu: %v", err)
		return nil, err
	}

	s.logger.Info().Msgf("gpu controller returned %v", resp)

	if !resp.Success {
		return nil, fmt.Errorf("could not restore gpu")
	}

	return gpuCmd, nil
}
