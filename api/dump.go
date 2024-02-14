package api

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/api/runc"
	"github.com/cedana/cedana/api/services/gpu"
	"github.com/cedana/cedana/api/services/task"
	container "github.com/cedana/cedana/container"
	"github.com/cedana/cedana/types"
	"github.com/cedana/cedana/utils"
	"github.com/checkpoint-restore/go-criu/v6/rpc"
	"github.com/docker/docker/pkg/namesgenerator"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// The bundle includes path to bundle and the runc/podman container id of the bundle. The bundle is a folder that includes the oci spec config.json
// as well as the rootfs used for setting up the container. Sometimes rootfs can be defined elsewhere. Podman adds extra directories and files in their
// bundle including a file called attach which is a unix socket for attaching stdin, stdout to the terminal
type Bundle struct {
	ContainerId string
	Bundle      string
}

// prepareDump =/= preDump.
// prepareDump sets up the folders to dump into, and sets the criu options.
// preDump on the other hand does any process cleanup right before the checkpoint.
func (c *Client) prepareDump(pid int32, dir string, opts *rpc.CriuOpts) (string, error) {
	var hasTCP bool
	var hasExtUnixSocket bool

	pname, err := utils.GetProcessName(pid)
	if err != nil {
		c.logger.Fatal().Err(err)
		return "", err
	}

	state, err := c.getState(pid)
	if state == nil || err != nil {
		return "", fmt.Errorf("could not get state")
	}

	for _, Conn := range state.ProcessInfo.OpenConnections {
		if Conn.Type == syscall.SOCK_STREAM { // TCP
			hasTCP = true
		}

		if Conn.Type == syscall.AF_UNIX { // Interprocess
			hasExtUnixSocket = true
		}
	}

	opts.TcpEstablished = proto.Bool(hasTCP)
	opts.ExtUnixSk = proto.Bool(hasExtUnixSocket)
	opts.FileLocks = proto.Bool(true)

	// check tty state
	// if pts is in open fds, chances are it's a shell job
	var isShellJob bool
	for _, f := range state.ProcessInfo.OpenFds {
		if strings.Contains(f.Path, "pts") {
			isShellJob = true
			break
		}
	}
	opts.ShellJob = proto.Bool(isShellJob)

	// processname + datetime
	// strip out non posix-compliant characters from the processname
	formattedProcessName := regexp.MustCompile("[^a-zA-Z0-9_.-]").ReplaceAllString(*pname, "_")
	formattedProcessName = strings.ReplaceAll(formattedProcessName, ".", "_")
	processCheckpointDir := strings.Join([]string{formattedProcessName, time.Now().Format("02_01_2006_1504")}, "_")
	checkpointFolderPath := filepath.Join(dir, processCheckpointDir)
	_, err = os.Stat(filepath.Join(checkpointFolderPath))
	if err != nil {
		if err := os.MkdirAll(checkpointFolderPath, 0o777); err != nil {
			return "", err
		}
	}

	err = chmodRecursive(checkpointFolderPath, 0o777)
	if err != nil {
		return "", err
	}

	// close common fds
	err = closeCommonFds(int32(os.Getpid()), pid)
	if err != nil {
		return "", err
	}

	// c.copyOpenFiles(checkpointFolderPath, state)

	return checkpointFolderPath, nil
}

func (c *Client) postDump(dumpdir string, state *task.ProcessState) {
	c.timers.Start(utils.CompressOp)
	compressedCheckpointPath := strings.Join([]string{dumpdir, ".tar"}, "")

	state.CheckpointPath = compressedCheckpointPath
	state.CheckpointState = task.CheckpointState_CHECKPOINTED
	// sneak in a serialized state obj
	err := c.SerializeStateToDir(dumpdir, state)
	if err != nil {
		c.logger.Fatal().Err(err)
	}

	c.logger.Info().Msgf("compressing checkpoint to %s", compressedCheckpointPath)

	err = utils.TarFolder(dumpdir, compressedCheckpointPath)
	if err != nil {
		c.logger.Fatal().Err(err)
	}

	err = c.db.UpdateProcessStateWithID(c.jobID, state)
	if err != nil {
		c.logger.Fatal().Err(err)
	}
	c.timers.Stop(utils.CompressOp)
}

func (c *Client) prepareCheckpointOpts() *rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel:     proto.Int32(4),
		LogFile:      proto.String("dump.log"),
		LeaveRunning: proto.Bool(c.config.Client.LeaveRunning),
		GhostLimit:   proto.Uint32(uint32(10000000)),
	}
	return &opts

}

func checkIfPodman(b Bundle) bool {
	var matched bool
	if b.ContainerId != "" {
		_, err := os.Stat(filepath.Join("/var/lib/containers/storage/overlay-containers/", b.ContainerId, "userdata"))
		return err == nil
	} else {
		pattern := "/var/lib/containers/storage/overlay-containers/.*?/userdata"
		matched, _ = regexp.MatchString(pattern, b.Bundle)
	}
	return matched
}

func patchPodmanDump(containerId, imgPath string) error {
	var containerStoreData *types.StoreContainer

	config := make(map[string]interface{})
	state := make(map[string]interface{})

	bundlePath := "/var/lib/containers/storage/overlay-containers/" + containerId + "/userdata"

	byteId := []byte(containerId)

	db := &utils.DB{Conn: nil, DbPath: "/var/lib/containers/storage/libpod/bolt_state.db"}

	if err := db.SetNewDbConn(); err != nil {
		return err
	}

	defer db.Conn.Close()

	err := db.Conn.View(func(tx *bolt.Tx) error {

		bkt, err := utils.GetCtrBucket(tx)
		if err != nil {
			return err
		}

		if err := db.GetContainerConfigFromDB(byteId, &config, bkt); err != nil {
			return err
		}

		if err := db.GetContainerStateDB(byteId, &state, bkt); err != nil {
			return err
		}

		utils.WriteJSONFile(config, imgPath, "config.dump")

		jsonPath := filepath.Join(bundlePath, "config.json")
		cfg, _, err := utils.NewFromFile(jsonPath)
		if err != nil {
			return err
		}

		utils.WriteJSONFile(cfg, imgPath, "spec.dump")

		return nil
	})

	ctrConfig := new(types.ContainerConfig)
	if _, err := utils.ReadJSONFile(ctrConfig, imgPath, "config.dump"); err != nil {
		return err
	}

	storeConfig := new([]types.StoreContainer)
	if _, err := utils.ReadJSONFile(storeConfig, utils.StorePath, "containers.json"); err != nil {
		return err
	}

	// Grabbing the state of the container in containers.json for the specific podman container
	for _, container := range *storeConfig {
		if container.ID == ctrConfig.ID {
			containerStoreData = &container
		}
	}
	name := namesgenerator.GetRandomName(0)

	containerStoreData.Names = []string{name}

	// Saving the current state of containers.json for the specific podman container we are checkpointing
	utils.WriteJSONFile(containerStoreData, imgPath, "containers.json")

	if err != nil {
		return err
	}
	return nil

}

func (c *Client) RuncDump(root, containerId string, opts *container.CriuOpts) error {
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
	bundle := Bundle{ContainerId: containerId}
	runcContainer := container.GetContainerFromRunc(containerId, root)
	err := runcContainer.RuncCheckpoint(opts, runcContainer.Pid, root, runcContainer.Config)
	if err != nil {
		c.logger.Fatal().Err(err)
	}

	if checkIfPodman(bundle) {
		if err := patchPodmanDump(containerId, opts.ImagesDirectory); err != nil {
			return err
		}
	}

	pid, err := runc.GetPidByContainerId(containerId, root)
	if err != nil {
		c.logger.Warn().Msgf("could not generate state: %v", err)
		return err
	}

	state, err := c.generateState(int32(pid))
	if err != nil {
		c.logger.Warn().Msgf("could not generate state: %v", err)
		return err
	}

	// CRIU ntfy hooks get run before this,
	// so have to ensure that image files aren't tampered with
	c.postDump(opts.ImagesDirectory, state)
	c.cleanupClient()

	return nil
}

func (c *Client) ContainerDump(imagePath, containerId string) error {
	root := "/run/containerd/runc/k8s.io"

	err := container.Dump(imagePath, containerId)
	if err != nil {
		c.logger.Fatal().Err(err)
		return err
	}

	pid, err := runc.GetPidByContainerId(containerId, root)
	if err != nil {
		c.logger.Warn().Msgf("could not generate state: %v", err)
		return err
	}

	state, err := c.generateState(int32(pid))
	if err != nil {
		c.logger.Warn().Msgf("could not generate state: %v", err)
		return err
	}

	// CRIU ntfy hooks get run before this,
	// so have to ensure that image files aren't tampered with
	c.postDump(imagePath, state)
	c.cleanupClient()

	return nil
}

func (c *Client) Dump(dir string, pid int32) error {
	defer c.timeTrack(time.Now(), "dump")

	opts := c.prepareCheckpointOpts()
	dumpdir, err := c.prepareDump(pid, dir, opts)
	if err != nil {
		return err
	}

	// TODO NR:add another check here for task running w/ accel resources
	var GPUCheckpointed bool
	if os.Getenv("CEDANA_GPU_ENABLED") == "true" {
		err = c.gpuCheckpoint(dumpdir)
		if err != nil {
			return err
		}
		GPUCheckpointed = true
		c.logger.Info().Msgf("gpu checkpointed, copying checkpoints...")
		// move gpu checkpoint files into workdir
		err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if strings.Contains(path, "gpuckpt") || strings.Contains(path, "mem") {
				c.logger.Info().Msgf("copying file %s to %s", path, dumpdir)
				err := utils.CopyFile(path, dumpdir)
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	img, err := os.Open(dumpdir)
	if err != nil {
		c.logger.Warn().Msgf("could not open checkpoint storage dir %s with error: %v", dir, err)
		return err
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))
	opts.Pid = proto.Int32(pid)

	nfy := Notify{
		Logger: c.logger,
	}

	c.logger.Info().Msgf(`beginning dump of pid %d`, pid)
	state, err := c.generateState(pid)
	if err != nil {
		c.logger.Warn().Msgf("could not generate state: %v", err)
		return err
	}

	c.timers.Start(utils.CriuCheckpointOp)
	_, err = c.CRIU.Dump(opts, &nfy)
	if err != nil {
		// check for sudo error
		if strings.Contains(err.Error(), "errno 0") {
			c.logger.Warn().Msgf("error dumping, cedana is not running as root: %v", err)
			return err
		}

		c.logger.Warn().Msgf("error dumping process: %v", err)
		return err
	}
	c.timers.Stop(utils.CriuCheckpointOp)

	state.GPUCheckpointed = GPUCheckpointed
	c.postDump(dumpdir, state)
	c.cleanupClient()

	return nil
}

func (c *Client) gpuCheckpoint(dumpdir string) error {
	// TODO NR - these should move out of here and be part of the Client lifecycle
	// setting up a connection could be a source of slowdown for checkpointing
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	gpuConn, err := grpc.Dial("127.0.0.1:50051", opts...)
	if err != nil {
		c.logger.Warn().Msgf("could not connect to gpu controller service: %v", err)
		return err
	}
	defer gpuConn.Close()

	gpuServiceConn := gpu.NewCedanaGPUClient(gpuConn)

	args := gpu.CheckpointRequest{
		Directory: dumpdir,
	}

	resp, err := gpuServiceConn.Checkpoint(c.ctx, &args)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("could not checkpoint gpu")
	}

	return nil
}
