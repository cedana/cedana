package api

import (
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

func (c *Client) prepareRestore(ctx context.Context, opts *rpc.CriuOpts, checkpointPath string) (*string, *task.ProcessState, []*os.File, error) {
	var isShellJob bool
	var inheritFds []*rpc.InheritFd
	var tcpEstablished bool
	var extraFiles []*os.File

	_, prepareRestoreSpan := c.tracer.Start(ctx, "prepare_restore")
	defer prepareRestoreSpan.End()
	tmpdir := "/tmp/cedana_restore"

	// check if tmpdir exists
	if _, err := os.Stat(tmpdir); os.IsNotExist(err) {
		err := os.Mkdir(tmpdir, 0755)
		if err != nil {
			return nil, nil, nil, err
		}
	} else {
		// likely an old checkpoint hanging around, delete
		err := os.RemoveAll(tmpdir)
		if err != nil {
			return nil, nil, nil, err
		}
		err = os.Mkdir(tmpdir, 0755)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	c.logger.Info().Msgf("decompressing %s to %s", checkpointPath, tmpdir)
	err := utils.UntarFolder(checkpointPath, tmpdir)

	if err != nil {
		c.logger.Fatal().Err(err).Msg("error decompressing checkpoint")
	}

	// read serialized cedanaCheckpoint
	_, err = os.Stat(filepath.Join(tmpdir, "checkpoint_state.json"))
	if err != nil {
		c.logger.Fatal().Err(err).Msg("checkpoint_state.json not found, likely error in creating checkpoint")
		return nil, nil, nil, err
	}

	data, err := os.ReadFile(filepath.Join(tmpdir, "checkpoint_state.json"))
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error reading checkpoint_state.json")
		return nil, nil, nil, err
	}

	var checkpointState task.ProcessState
	err = json.Unmarshal(data, &checkpointState)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error unmarshaling checkpoint_state.json")
		return nil, nil, nil, err
	}

	open_fds := checkpointState.ProcessInfo.OpenFds

	// create logfile for redirection
	filename := fmt.Sprintf("/var/log/cedana-output-%s.log", fmt.Sprint(time.Now().Unix()))
	file, err := os.Create(filename)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error creating logfile")
		return nil, nil, nil, err
	}

	for _, f := range open_fds {
		if strings.Contains(f.Path, "pts") {
			isShellJob = true
		}
		// if stdout or stderr, always redirect fds
		if f.Stream == task.OpenFilesStat_STDOUT || f.Stream == task.OpenFilesStat_STDERR {
			// strip leading slash from f
			f.Path = strings.TrimPrefix(f.Path, "/")

			extraFiles = append(extraFiles, file)
			inheritFds = append(inheritFds, &rpc.InheritFd{
				Fd:  proto.Int32(2 + int32(len(extraFiles))),
				Key: proto.String(f.Path),
			})
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
	opts.TcpEstablished = proto.Bool(tcpEstablished)

	if err := chmodRecursive(tmpdir, 0o777); err != nil {
		c.logger.Fatal().Err(err).Msg("error changing permissions")
		return nil, nil, nil, err
	}

	return &tmpdir, &checkpointState, extraFiles, nil
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

func (c *Client) ContainerRestore(imgPath string, containerId string) error {
	logger := utils.GetLogger()
	logger.Info().Msgf("restoring container %s from %s", containerId, imgPath)
	err := container.Restore(imgPath, containerId)
	if err != nil {
		return err
	}
	return nil
}

// restoreFiles looks at the files copied during checkpoint and copies them back to the
// original path, creating folders along the way.
func (c *Client) restoreFiles(ps *task.ProcessState, dir string) {
	_, err := os.Stat(dir)
	if err != nil {
		return
	}
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		for _, f := range ps.ProcessInfo.OpenFds {
			if info.Name() == filepath.Base(f.Path) {
				// copy file to path
				err = os.MkdirAll(filepath.Dir(f.Path), 0755)
				if err != nil {
					return err
				}

				c.logger.Info().Msgf("copying file %s to %s", path, f)
				// copyFile copies to folder, so grab folder path
				err := utils.CopyFile(path, filepath.Dir(f.Path))
				if err != nil {
					return err
				}

			}
		}
		return nil
	})

	if err != nil {
		c.logger.Fatal().Err(err).Msg("error copying files")
	}
}

func (c *Client) prepareRestoreOpts() *rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel: proto.Int32(4),
		LogFile:  proto.String("cedana-restore.log"),
	}

	return &opts

}

func (c *Client) criuRestore(ctx context.Context, opts *rpc.CriuOpts, nfy Notify, dir string, extraFiles []*os.File) (*int32, error) {
	_, restoreSpan := c.tracer.Start(ctx, "restore")
	restoreSpan.SetAttributes(attribute.Bool("container", false))
	defer restoreSpan.End()

	img, err := os.Open(dir)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("could not open directory")
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))

	resp, err := c.CRIU.Restore(opts, &nfy, extraFiles)
	if err != nil {
		// cleanup along the way
		os.RemoveAll(dir)
		c.logger.Warn().Msgf("error restoring process: %v", err)
		restoreSpan.RecordError(err)
		return nil, err
	}

	c.logger.Info().Msgf("process restored: %v", resp)

	c.cleanupClient()
	return resp.Restore.Pid, nil
}

func patchPodmanRestore(ctx context.Context, opts *container.RuncOpts, containerId, imgPath string) error {
	// Podman run -d state
	if !opts.Detatch {
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

func copyFiles(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Construct the destination path by joining the destination directory
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, relPath)

		// If it's a directory, create it in the destination
		if info.IsDir() {
			return os.MkdirAll(destPath, os.ModePerm)
		}

		// If it's a file, copy it to the destination
		if !info.Mode().IsRegular() {
			return nil
		}

		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer sourceFile.Close()

		destFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, sourceFile)
		return err
	})
}

type linkPairs struct {
	Key   string
	Value string
}

func (c *Client) RuncRestore(ctx context.Context, imgPath, containerId string, isK3s bool, sources []string, opts *container.RuncOpts) error {
	ctx, restoreSpan := c.tracer.Start(ctx, "restore")
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
		//ctx := namespaces.WithNamespace(context.Background(), "k8s.io")
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

	go func() {
		if isPodman {
			if err := patchPodmanRestore(ctx, opts, containerId, imgPath); err != nil {
				log.Fatal(err)
			}
		}
	}()
	return nil
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

func (c *Client) Restore(ctx context.Context, args *task.RestoreArgs) (*int32, error) {
	var dir *string
	var pid *int32

	opts := c.prepareRestoreOpts()
	nfy := Notify{
		Logger: c.logger,
	}

	dir, state, extraFiles, err := c.prepareRestore(ctx, opts, args.CheckpointPath)
	if err != nil {
		return nil, err
	}

	var gpuCmd *exec.Cmd
	if state.GPUCheckpointed {
		nfy.PreResumeFunc = NotifyFunc{
			Avail: true,
			Callback: func() error {
				var err error
				gpuCmd, err = c.gpuRestore(ctx, *dir)
				return err
			},
		}
	}

	pid, err = c.criuRestore(ctx, opts, nfy, *dir, extraFiles)
	if err != nil {
		return nil, err
	}

	if state.GPUCheckpointed {
		go func() {
			proc, err := process.NewProcess(*pid)
			if err != nil {
				c.logger.Error().Msgf("could not find process: %v", err)
				return
			}
			for {
				running, err := proc.IsRunning()
				if err != nil || !running {
					break
				}
				time.Sleep(1 * time.Second)
			}
			c.logger.Debug().Msgf("process %d exited, killing gpu-controller", *pid)
			gpuCmd.Process.Kill()
		}()
	}

	return pid, nil
}

func (c *Client) gpuRestore(ctx context.Context, dir string) (*exec.Cmd, error) {
	ctx, gpuSpan := c.tracer.Start(ctx, "gpu-restore")
	defer gpuSpan.End()
	// TODO NR - propagate uid/guid too
	gpuCmd, err := StartGPUController(1001, 1001, c.logger)
	if err != nil {
		c.logger.Warn().Msgf("could not start gpu-controller: %v", err)
		return nil, err
	}

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	// sleep a little to let the gpu controller start
	time.Sleep(10 * time.Millisecond)

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
		c.logger.Warn().Msgf("could not restore gpu: %v", err)
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("could not restore gpu")
	}

	return gpuCmd, nil
}
