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
	"strconv"
	"strings"
	"time"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
	"github.com/checkpoint-restore/go-criu/v6/rpc"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

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
	var tmpSources string
	var sandboxID string
	var sourceList []string
	var pauseNetNs string
	var nsPath string
	var spec rspec.Spec
	var configLocation string
	if len(sources) > 0 {
		tmpSources = filepath.Join("/tmp", "sources")

		defer os.Remove(tmpSources)
		configLocation = filepath.Join(opts.Bundle, "config.json")

		_, err := os.Stat(configLocation)
		if err == nil {
			configFile, err := os.ReadFile(configLocation)
			if err != nil {
				return err
			}

			if err := json.Unmarshal(configFile, &spec); err != nil {
				return err
			}
		}
		sandboxID = spec.Annotations["io.kubernetes.cri.sandbox-id"]
		// podID := spec.Annotations["io.kubernetes.cri.sandbox-uid"]
		var networkIndex int
		for i, ns := range spec.Linux.Namespaces {
			if ns.Type == "network" {
				nsPath = ns.Path
				networkIndex = i
				break
			}
		}

		spec.Linux.Namespaces[networkIndex].Path = filepath.Join("/host", nsPath)
		specJson, err := json.Marshal(&spec)
		if err != nil {
			return err
		}

		if err := os.WriteFile(configLocation, specJson, 0777); err != nil {
			return err
		}

		// This is to bypass the issue with runc restore not finding sources
		for i, m := range spec.Mounts {
			if strings.HasPrefix(m.Source, "/") {
				spec.Mounts[i].Source = filepath.Join("/host", m.Source)
			}
		}

		for i, s := range sources {
			sourceList = append(sourceList, filepath.Join("/tmp", "sources", fmt.Sprint(sandboxID, "-", i)))

			if err := copyFiles(filepath.Join(s, sandboxID), sourceList[i]); err != nil {
				return err
			}
		}
		if err := copyFiles(opts.Bundle, "/tmp/sources/bundle"); err != nil {
			return err
		}
	}

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
		}
		// Create sym links so that runc c/r can resolve config.json paths to the mounted ones in /host
		for _, link := range links {
			if _, err := os.Stat(link.Value); err != nil {
				if err := os.Symlink(link.Key, link.Value); err != nil {
					return err
				}
			}
		}
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

		// Generate new IDs
		// newPodID := uuid.New().String()
		// newContainerID := uuid.New().String()

		// var directories []string

		sandboxID := spec.Annotations["io.kubernetes.cri.sandbox-id"]
		podID := spec.Annotations["io.kubernetes.cri.sandbox-uid"]
		tmpPath := filepath.Join("/tmp", podID)
		if err := os.Mkdir(tmpPath, 0644); err != nil {
			return err
		}
		tmpPodsPath := filepath.Join(tmpPath, "pods")
		podsPath := filepath.Join("/host/var/lib/kubelet/pods", podID)
		if err := rsyncDirectories(podsPath, tmpPodsPath); err != nil {
			return err
		}
		tmpVarPath := filepath.Join(tmpPath, "var")
		varPath := filepath.Join("/host/var/lib/rancher/k3s/agent/containerd/io.containerd.grpc.v1.cri/sandboxes", sandboxID)
		if err := rsyncDirectories(varPath, tmpVarPath); err != nil {
			return err
		}
		tmpRunPath := filepath.Join(tmpPath, "run")
		runPath := filepath.Join("/host/run/k3s/containerd/io.containerd.grpc.v1.cri/sandboxes", sandboxID)
		if err := rsyncDirectories(runPath, tmpRunPath); err != nil {
			return err
		}

		tmpBesteffortPath := filepath.Join(tmpPath, "besteffort")
		besteffortPath := filepath.Join("/host/kubepods/besteffort", podID)
		rsyncDirectories(besteffortPath, tmpBesteffortPath)

		tmpBundlePath := filepath.Join(tmpPath, "bundle")
		if err := rsyncDirectories(opts.Bundle, tmpBundlePath); err != nil {
			return err
		}

		var pausePid int
		// TODO before killing runc container, need to do a checkpoint on pause container
		// Checkpoint pause container
		// Restore pause container
		// Get pid of pause container
		// Patch config.json to reflect new pid in namespaces

		// Looping through namespaces taken from the spec of the container we are trying to
		// checkpoint.
		for _, ns := range spec.Linux.Namespaces {
			if ns.Type == "network" {
				// Looking for the pid of the pause container from the path to the network namespace
				split := strings.Split(ns.Path, "/")
				pausePid, err = strconv.Atoi(split[2])
				if err != nil {
					return err
				}
			}
		}
		ctrs, err := container.GetContainers(opts.Root)
		if err != nil {
			return err
		}

		var pauseContainer container.ContainerStateJson

		for _, c := range ctrs {
			if c.InitProcessPid == pausePid {
				pauseContainer = c
			}
		}

		pauseContainerRestoreOpts := &container.RuncOpts{
			Root:    opts.Root,
			Bundle:  "/host" + pauseContainer.Bundle,
			Detatch: true,
			Pid:     pausePid,
		}

		pauseContainerDumpOpts := &container.CriuOpts{
			LeaveRunning:    true,
			TcpEstablished:  false,
			ImagesDirectory: "/tmp/pause_checkpoint",
		}
		pauseNetNs = filepath.Join("/proc", strconv.Itoa(pausePid), "ns", "net")

		if err := c.RuncDump("/host/run/containerd/runc/k8s.io", pauseContainer.ID, pauseContainerDumpOpts); err != nil {
			return err
		}

		if err := os.Mkdir("/tmp/sources", 0644); err != nil {
			return err
		}

		// TODO find a more general way to do the rsync copy to and from tmp for k8s files to do proper restore
		pauseSources := &[]string{"/host/run/k3s/containerd/io.containerd.grpc.v1.cri/sandboxes/", "/host/var/lib/rancher/k3s/agent/containerd/io.containerd.grpc.v1.cri/sandboxes/"}

		if err := c.RuncRestore("/tmp/pause_checkpoint", pauseContainer.ID, false, *pauseSources, pauseContainerRestoreOpts); err != nil {
			return err
		}

		killRuncContainer(sandboxID)
		// // Update paths and perform recursive copy
		// err = updateAndCopyDirectories(config, "/tmp", "podd7f6555a-8d1e-46ae-b97a-7c3639682bbb", newPodID, "52f274894cf23cd0e23192ef00ce2a7615cb548f30b9f5517dc7324d9611e4da", newContainerID)
		// if err != nil {
		// 	fmt.Println("Error copying directories:", err)
		// }

		cleanPodsPath, err := dropLastDirectory(podsPath)
		if err != nil {
			return err
		}
		if err := copyFiles(tmpPodsPath, cleanPodsPath); err != nil {
			return err
		}
		cleanVarPath, err := dropLastDirectory(varPath)
		if err != nil {
			return err
		}
		if err := copyFiles(tmpVarPath, cleanVarPath); err != nil {
			return err
		}
		cleanRunPath, err := dropLastDirectory(runPath)
		if err != nil {
			return err
		}
		if err := copyFiles(tmpRunPath, cleanRunPath); err != nil {
			return err
		}

		if err := copyFiles(tmpBesteffortPath, besteffortPath); err != nil {
			return err
		}
		cleanBundlePath, err := dropLastDirectory(opts.Bundle)
		if err != nil {
			return err
		}
		if err := copyFiles(tmpBundlePath, cleanBundlePath); err != nil {
			return err
		}
	}

	if len(sources) > 0 {
		pauseNetNs = filepath.Join("/proc", strconv.Itoa(opts.Pid), "ns", "net")
		parts := strings.Split(nsPath, "/")
		parts = parts[:len(parts)-1]
		id := generateCustomID()
		parts = append(parts, id)
		newNsPath := strings.Join(parts, "/")
		file, err := os.Create("/host/" + newNsPath)
		if err != nil {
			return err
		}
		defer file.Close()
		if err := mount(pauseNetNs, "/host/"+newNsPath); err != nil {
			return err
		}

		killRuncContainer(sandboxID)

		for i, ns := range spec.Linux.Namespaces {
			if ns.Type == "network" {
				spec.Linux.Namespaces[i].Path = "/host" + newNsPath
			}
		}

		specJson, err := json.Marshal(&spec)
		if err != nil {
			return err
		}

		if err := os.WriteFile("/tmp/sources/bundle/config.json", specJson, 0644); err != nil {
			return err
		}

		for i, s := range sources {
			if err := copyFiles(filepath.Join(tmpSources, fmt.Sprint(sandboxID, "-", i)), filepath.Join(s, sandboxID)); err != nil {
				return err
			}
		}
		// Copy bundle over
		if err := copyFiles("/tmp/sources/bundle", filepath.Join("/host/run/k3s/containerd/io.containerd.runtime.v2.task/k8s.io/", sandboxID)); err != nil {
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
	defer func() {
		os.RemoveAll(filepath.Join("/tmp", sandboxID))
		os.RemoveAll(filepath.Join("/tmp", "pause_checkpoint"))
		os.RemoveAll(filepath.Join("/tmp", "sources"))
		umount("/tmp/sources/netns")
	}()
	return nil
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
