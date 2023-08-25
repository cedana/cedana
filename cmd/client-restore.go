package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cedana/cedana/utils"
	"github.com/checkpoint-restore/go-criu/v5/rpc"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	cedana "github.com/cedana/cedana/types"
)

func init() {
	clientCommand.AddCommand(restoreCmd)
}

// Restore command is only called when run manually.
// Otherwise, daemon command is used and a network/cedana-managed restore occurs.
var restoreCmd = &cobra.Command{
	Use:   "restore",
	Args:  cobra.ExactArgs(1),
	Short: "Initialize client and restore from dumped image",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := instantiateClient()
		if err != nil {
			return err
		}

		checkpointPath := args[0]
		// check if it exists before passing to restore
		_, err = os.Stat(checkpointPath)
		if err != nil {
			return err
		}

		err = c.restore(nil, &checkpointPath)
		if err != nil {
			return err
		}
		return nil
	},
}

func (c *Client) prepareRestore(opts *rpc.CriuOpts, cmd *cedana.ServerCommand, checkpointPath string) (*string, error) {
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

func (c *Client) postRestore() {
	// get PID of freshly restored process

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

func (c *Client) prepareRestoreOpts() rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel:       proto.Int32(4),
		LogFile:        proto.String("restore.log"),
		TcpEstablished: proto.Bool(true),
	}

	return opts

}

func (c *Client) criuRestore(opts *rpc.CriuOpts, nfy utils.Notify, dir string) error {

	img, err := os.Open(dir)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("could not open directory")
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))

	err = c.CRIU.Restore(opts, nfy)
	if err != nil {
		// cleanup along the way
		os.RemoveAll(dir)
		c.logger.Warn().Msgf("error restoring process: %v", err)
		return err
	}

	// clean up
	err = os.RemoveAll(dir)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error removing directory")
	}
	c.cleanupClient()
	return nil
}

func (c *Client) pyTorchRestore() error {
	return nil
}

func (c *Client) restore(cmd *cedana.ServerCommand, path *string) error {
	defer c.timeTrack(time.Now(), "restore")
	var dir string

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
			tmpdir, err := c.prepareRestore(&opts, cmd, "")
			if err != nil {
				return err
			}
			dir = *tmpdir

			err = c.criuRestore(&opts, nfy, dir)
			if err != nil {
				return err
			}

		case cedana.CheckpointTypePytorch:
			err := c.pyTorchRestore()
			if err != nil {
				return err
			}
		}
	} else {
		dir, err := c.prepareRestore(&opts, nil, *path)
		if err != nil {
			return err
		}
		err = c.criuRestore(&opts, nfy, *dir)
		if err != nil {
			return err
		}
	}

	return nil
}
