package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
	"github.com/checkpoint-restore/go-criu/v6/rpc"
	"google.golang.org/protobuf/proto"

	cedana "github.com/cedana/cedana/types"
)

func (c *Client) prepareRestore(opts *rpc.CriuOpts, args *task.RestoreArgs, checkpointPath string) (*string, error) {
	tmpdir := "cedana_restore"
	// make temporary folder to decompress into
	err := os.Mkdir(tmpdir, 0755)
	if err != nil {
		return nil, err
	}

	var zipFile string
	if args.Dir != "" {
		// TODO BS I think this section might not make sense anymore w/o nats
		file, err := c.store.GetCheckpoint(args.Dir)
		if err != nil {
			return nil, err
		}
		if file != nil {
			zipFile = *file
		}
	} else {
		zipFile = checkpointPath
	}

	c.logger.Info().Msgf("decompressing %s to %s", zipFile, tmpdir)
	err = utils.UnzipFolder(zipFile, tmpdir)
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

	var checkpointState cedana.CedanaState
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
func (c *Client) restoreFiles(cc *cedana.CedanaState, dir string) {
	_, err := os.Stat(dir)
	if err != nil {
		return
	}
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		for _, f := range cc.ProcessInfo.OpenWriteOnlyFilePaths {
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

func (c *Client) RuncRestore(imgPath string, containerId string, opts *container.RuncOpts) error {

	err := container.RuncRestore(imgPath, containerId, *opts)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) prepareNatsRestore(opts *rpc.CriuOpts, cmd *cedana.ServerCommand, checkpointPath string) (*string, error) {
	tmpdir := "cedana_restore"
	// make temporary folder to decompress into
	err := os.Mkdir(tmpdir, 0755)
	if err != nil {
		return nil, err
	}

	var zipFile string
	if cmd != nil {
		file, err := c.store.GetCheckpoint(cmd.CedanaState.CheckpointPath)
		if err != nil {
			return nil, err
		}
		if file != nil {
			zipFile = *file
		}

	} else {
		zipFile = checkpointPath
	}
	c.logger.Info().Msgf("decompressing %s to %s", zipFile, tmpdir)
	err = utils.UnzipFolder(zipFile, tmpdir)
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

	var checkpointState cedana.CedanaState
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

func (c *Client) NatsRestore(cmd *cedana.ServerCommand, path *string) (*int32, error) {
	defer c.timeTrack(time.Now(), "restore")
	var dir string
	var pid *int32

	opts := c.prepareRestoreOpts()
	nfy := utils.Notify{
		Config:          c.config,
		Logger:          c.logger,
		PreDumpAvail:    true,
		PostDumpAvail:   true,
		PreRestoreAvail: true,
	}

	// if we have a server command, otherwise default to base CRIU wrapper mode
	if cmd != nil {
		switch cmd.CedanaState.CheckpointType {
		case cedana.CheckpointTypeCRIU:
			tmpdir, err := c.prepareNatsRestore(opts, cmd, "")
			if err != nil {
				return nil, err
			}
			dir = *tmpdir

			pid, err = c.criuRestore(opts, nfy, dir)
			if err != nil {
				return nil, err
			}

		case cedana.CheckpointTypePytorch:
			err := c.pyTorchRestore()
			if err != nil {
				return nil, err
			}
		}
	} else {
		dir, err := c.prepareRestore(opts, nil, *path)
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

func (c *Client) Restore(args *task.RestoreArgs) (*int32, error) {
	defer c.timeTrack(time.Now(), "restore")
	var dir string
	var pid *int32

	opts := c.prepareRestoreOpts()
	nfy := utils.Notify{
		Config:          c.config,
		Logger:          c.logger,
		PreDumpAvail:    true,
		PostDumpAvail:   true,
		PreRestoreAvail: true,
	}

	// if we have a server command, otherwise default to base CRIU wrapper mode
	switch args.Type {
	case task.RestoreArgs_PROCESS:
		tmpdir, err := c.prepareRestore(opts, args, "")
		if err != nil {
			return nil, err
		}
		dir = *tmpdir

		pid, err = c.criuRestore(opts, nfy, dir)
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
