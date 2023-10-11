package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
	"github.com/checkpoint-restore/go-criu/v6/rpc"
	"github.com/containers/podman/v4/cmd/podman/registry"
	"github.com/containers/podman/v4/libpod"
	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/checkpoint"
	"github.com/containers/podman/v4/pkg/domain/entities"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/containers/podman/v4/pkg/specgen/generate"
	"github.com/containers/podman/v4/pkg/specgenutil"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

func (c *Client) prepareRestore(opts *rpc.CriuOpts, args *task.RestoreArgs, checkpointPath string) (*string, error) {
	// Here we just want to call store.GetCheckpoint
	// setting auth token for now
	c.config.Connection.CedanaAuthToken = "brandonsmith"
	c.config.Connection.CedanaUrl = "http://localhost:1324"
	c.store = utils.NewCedanaStore(c.config)
	zipFile, err := c.store.GetCheckpoint(args.Cid)
	if err != nil {
		return nil, err
	}

	tmpdir := "cedana_restore"
	// make temporary folder to decompress into
	err = os.Mkdir(tmpdir, 0755)
	if err != nil {
		return nil, err
	}

	c.logger.Info().Msgf("decompressing %s to %s", *zipFile, tmpdir)
	err = utils.UnzipFolder(*zipFile, tmpdir)
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

	// TODO: network restore logic
	// TODO: checksum val

	// Remove for now for testing
	// err = os.Remove(zipFile)
	if err != nil {
		return nil, err
	}

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

func (c *Client) pyTorchRestore() error {
	// TODO Not implemented yet
	return nil
}

func (c *Client) patchPodman(rootfs string) error {
	ctx := registry.GetContext()
	var (
		runOpts entities.ContainerRunOptions
		ctrs    []*libpod.Container
		opts    entities.ContainerCreateOptions
	)

	runOpts.CIDFile = ""
	runOpts.Rm = false
	runOpts.OutputStream = os.Stdout
	runOpts.InputStream = os.Stdin
	runOpts.ErrorStream = os.Stderr

	s := specgen.NewSpecGenerator(rootfs, true)
	if err := specgenutil.FillOutSpecGen(s, &opts, []string{rootfs}); err != nil {
		return err
	}

	s.RawImageName = "test"

	registry.ContainerEngine().ContainerRun(ctx, runOpts)
	Libpod := &libpod.Runtime{}

	img, resolvedImageName := runOpts.Spec.GetImage()

	if img != nil && resolvedImageName != "" {
		_, err := img.Inspect(ctx, nil)
		if err != nil {
			return err
		}
	}
	namesOrIds := []string{resolvedImageName}

	warn, err := generate.CompleteSpec(ctx, Libpod, runOpts.Spec)
	if err != nil {
		return err
	}
	// Print warnings
	for _, w := range warn {
		fmt.Fprintf(os.Stderr, "%s\n", w)
	}

	var restoreOptions entities.RestoreOptions
	restoreOptions.Name = runOpts.Spec.Name
	restoreOptions.Pod = runOpts.Spec.Pod

	filterFuncs := []libpod.ContainerFilter{
		func(c *libpod.Container) bool {
			state, _ := c.State()
			return state == define.ContainerStateExited
		},
	}

	newRestoreOptions := libpod.ContainerCheckpointOptions{
		Keep:            restoreOptions.Keep,
		TCPEstablished:  restoreOptions.TCPEstablished,
		TargetFile:      restoreOptions.Import,
		Name:            restoreOptions.Name,
		IgnoreRootfs:    restoreOptions.IgnoreRootFS,
		IgnoreVolumes:   restoreOptions.IgnoreVolumes,
		IgnoreStaticIP:  restoreOptions.IgnoreStaticIP,
		IgnoreStaticMAC: restoreOptions.IgnoreStaticMAC,
		ImportPrevious:  restoreOptions.ImportPrevious,
		Pod:             restoreOptions.Pod,
		PrintStats:      restoreOptions.PrintStats,
		FileLocks:       restoreOptions.FileLocks,
	}

	idToRawInput := map[string]string{}
	switch {
	case restoreOptions.Import != "":
		ctrs, err = checkpoint.CRImportCheckpointTar(ctx, Libpod, restoreOptions)
	case restoreOptions.All:
		ctrs, err = Libpod.GetContainers(false, filterFuncs...)
	default:
		for _, nameOrID := range namesOrIds {
			logrus.Debugf("look up container: %q", nameOrID)
			c, err := Libpod.LookupContainer(nameOrID)
			if err == nil {
				ctrs = append(ctrs, c)
				idToRawInput[c.ID()] = nameOrID
			} else {
				// If container was not found, check if this is a checkpoint image
				logrus.Debugf("look up image: %q", nameOrID)
				img, _, err := Libpod.LibimageRuntime().LookupImage(nameOrID, nil)
				if err != nil {
					return fmt.Errorf("no such container or image: %s", nameOrID)
				}
				newRestoreOptions.CheckpointImageID = img.ID()
				mountPoint, err := img.Mount(ctx, nil, "")
				defer func() {
					if err := img.Unmount(true); err != nil {
						logrus.Errorf("Failed to unmount image: %v", err)
					}
				}()
				if err != nil {
					return err
				}
				importedCtrs, err := checkpoint.CRImportCheckpoint(ctx, Libpod, restoreOptions, mountPoint)
				if err != nil {
					// CRImportCheckpoint is expected to import exactly one container from checkpoint image
					return fmt.Errorf("unable to import checkpoint from image: %q: %v", nameOrID, err)
				} else {
					ctrs = append(ctrs, importedCtrs[0])
				}
			}
		}
	}
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) RuncRestore(imgPath, containerId string, opts *container.RuncOpts) error {

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

	if err := c.patchPodman(strings.Split(opts.Bundle, "/")[8]); err != nil {
		return err
	}

	if err := container.RuncRestore(imgPath, containerId, *opts); err != nil {
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

	switch args.Type {
	case task.RestoreArgs_PROCESS:
		tmpdir, err := c.prepareRestore(opts, args, "")
		if err != nil {
			return nil, err
		}
		dir = tmpdir

		pid, err = c.criuRestore(opts, nfy, *dir)
		if err != nil {
			return nil, err
		}
	case task.RestoreArgs_PYTORCH:
		err := c.pyTorchRestore()
		if err != nil {
			return nil, err
		}
	default:
		dir, err := c.prepareRestore(opts, nil, args.Dir)
		if err != nil {
			return nil, err
		}
		pid, err = c.criuRestore(opts, nfy, *dir)
		if err != nil {
			return nil, err
		}
	}

	return pid, nil
}
