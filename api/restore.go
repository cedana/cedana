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
	"time"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
	"github.com/checkpoint-restore/go-criu/v6/rpc"
	"github.com/containerd/containerd/identifiers"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/typeurl/v2"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/opencontainers/runtime-spec/specs-go"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
)

func (c *Client) prepareRestore(opts *rpc.CriuOpts, args *task.RestoreArgs, checkpointPath string) (*string, error) {
	tmpdir := "cedana_restore"
	// make temporary folder to decompress into
	err := os.Mkdir(tmpdir, 0755)
	if err != nil {
		return nil, err
	}

	c.logger.Info().Msgf("decompressing %s to %s", checkpointPath, tmpdir)
	err = utils.UnzipFolder(checkpointPath, tmpdir)
	if err != nil {
		// hack: error here is not fatal due to EOF (from a badly written utils.Compress)
		c.logger.Info().Err(err).Msg("error decompressing checkpoint")
	}

	// read serialized cedanaCheckpoint
	_, err = os.Stat(filepath.Join(tmpdir, "checkpoint_state.json"))
	if err != nil {
		c.logger.Fatal().Err(err).Msg("checkpoint_state.json not found, likely error in creating checkpoint")
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(tmpdir, "checkpoint_state.json"))
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error reading checkpoint_state.json")
		return nil, err
	}

	var checkpointState task.ProcessState
	err = json.Unmarshal(data, &checkpointState)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error unmarshaling checkpoint_state.json")
		return nil, err
	}

	// check open_fds. Useful for checking if process being restored
	// is a pts slave and for determining how to handle files that were being written to.
	// TODO: We should be looking at the images instead
	open_fds := checkpointState.ProcessInfo.OpenFds
	var isShellJob bool
	for _, f := range open_fds {
		if strings.Contains(f.Path, "pts") {
			isShellJob = true
			break
		}
	}
	opts.ShellJob = proto.Bool(isShellJob)

	c.restoreFiles(&checkpointState, tmpdir)

	return &tmpdir, nil
}

func (c *Client) ContainerRestore(imgPath string, containerId string) error {
	logger := utils.GetLogger()
	logger.Info().Msgf("restoring container %s from %s", containerId, imgPath)
	err := container.Restore(imgPath, containerId)
	if err != nil {
		c.logger.Fatal().Err(err)
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
		for _, f := range ps.ProcessInfo.OpenWriteOnlyFilePaths {
			if info.Name() == filepath.Base(f) {
				// copy file to path
				err = os.MkdirAll(filepath.Dir(f), 0755)
				if err != nil {
					return err
				}

				c.logger.Info().Msgf("copying file %s to %s", path, f)
				// copyFile copies to folder, so grab folder path
				err := utils.CopyFile(path, filepath.Dir(f))
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
		LogLevel:       proto.Int32(2),
		LogFile:        proto.String("restore.log"),
		TcpEstablished: proto.Bool(true),
	}

	return &opts

}

func (c *Client) criuRestore(opts *rpc.CriuOpts, nfy utils.Notify, dir string) (*int32, error) {

	img, err := os.Open(dir)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("could not open directory")
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))

	resp, err := c.CRIU.Restore(opts, &nfy)
	if err != nil {
		// cleanup along the way
		os.RemoveAll(dir)
		c.logger.Warn().Msgf("error restoring process: %v", err)
		return nil, err
	}

	c.logger.Info().Msgf("process restored: %v", resp)

	// clean up
	err = os.RemoveAll(dir)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error removing directory")
	}
	c.cleanupClient()
	return resp.Restore.Pid, nil
}

func patchPodmanRestore(opts *container.RuncOpts, containerId, imgPath string) error {
	ctx := context.Background()

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

	// Here is the podman patch
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

func killRuncContainer(containerID string) error {
	cmd := exec.Command("sudo", "/host/bin/runc", "--root", "/host/run/containerd/runc/k8s.io", "kill", containerID)

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
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

func dropLastDirectory(path string) (string, error) {
	cleanPath := filepath.Clean(path)
	parentDir := filepath.Dir(cleanPath)

	// Check if the path is root directory
	if parentDir == cleanPath {
		return "", fmt.Errorf("cannot drop last directory of root directory")
	}

	return parentDir, nil
}

func mount(src, tgt string) error {
	cmd := exec.Command("mount", "--bind", src, tgt)
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}
func umount(tgt string) error {
	cmd := exec.Command("umount", tgt)
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}
func generateCustomID() string {
	uuidObj := uuid.New()
	// Extract specific segments from the generated UUID
	id := fmt.Sprintf("cni-%s", uuidObj.String())
	return id
}

type linkPairs struct {
	Key   string
	Value string
}

func (c *Client) RuncRestore(imgPath, containerId string, isK3s bool, sources []string, opts *container.RuncOpts) error {

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
			if err := patchPodmanRestore(opts, containerId, imgPath); err != nil {
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

func (c *Client) Restore(args *task.RestoreArgs) (*int32, error) {
	defer c.timeTrack(time.Now(), "restore")
	var dir *string
	var pid *int32

	opts := c.prepareRestoreOpts()
	nfy := utils.Notify{
		Config:          c.config,
		Logger:          c.logger,
		PreDumpAvail:    true,
		PostDumpAvail:   true,
		PreRestoreAvail: true,
	}

	dir, err := c.prepareRestore(opts, nil, args.CheckpointPath)
	if err != nil {
		return nil, err
	}
	pid, err = c.criuRestore(opts, nfy, *dir)
	if err != nil {
		return nil, err
	}

	return pid, nil
}
