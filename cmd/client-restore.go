package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/nravic/cedana/utils"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

var checkpoint string

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

func (c *Client) prepareRestore(opts *rpc.CriuOpts, checkpointPath string) (*string, error) {
	tmpdir := "cedana_restore"
	// make temporary folder to decompress into
	err := os.Mkdir(tmpdir, 0755)
	if err != nil {
		return nil, err
	}

	c.logger.Info().Msgf("decompressing %s to %s", checkpointPath, tmpdir)
	err = utils.DecompressFolder(checkpointPath, tmpdir)
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

	var cedanaCheckpoint CedanaCheckpoint
	err = json.Unmarshal(data, &cedanaCheckpoint)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error unmarshaling checkpoint_state.json")
		return nil, err
	}

	// check open_fds. Useful for checking if process being restored
	// is a pts slave and for determining how to handle files that were being written to.
	// TODO: We should be looking at the images instead
	open_fds := cedanaCheckpoint.ClientState.ProcessInfo.OpenFds
	var isShellJob bool
	for _, f := range open_fds {
		if strings.Contains(f.Path, "pts") {
			isShellJob = true
			break
		}
	}
	opts.ShellJob = proto.Bool(isShellJob)

	// TODO: network restore logic
	// TODO: checksum val

	return &tmpdir, nil
}

// restore function for systems managed by a cedana orchestrator
func (c *Client) prepareManagedRestore(opts *rpc.CriuOpts, cmd *ServerCommand) (*string, error) {
	checkpoint := cmd.CedanaCheckpoint

	var isShellJob bool
	// check openFds at time of checkpoint
	fds := checkpoint.ClientState.ProcessInfo.OpenFds
	for _, f := range fds {
		if strings.Contains(f.Path, "pts") {
			isShellJob = true
			break
		}
	}
	opts.ShellJob = proto.Bool(isShellJob)

	file, err := c.getCheckpointFile(checkpoint.CheckpointPath)
	if err != nil {
		return nil, err
	}

	tmpdir := "cedana_restore"
	// make temporary folder to decompress into
	err = os.Mkdir(tmpdir, 0755)
	if err != nil {
		return nil, err
	}

	err = utils.UnzipFolder(*file, tmpdir)
	if err != nil {
		// hack: error here is not fatal due to EOF (from a badly written utils.Compress)
		c.logger.Info().Err(err).Msg("error decompressing checkpoint")
	}

	// clean up
	err = os.Remove(*file)
	if err != nil {
		return nil, err
	}

	return &tmpdir, nil
}

func (c *Client) prepareRestoreOpts() rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel:       proto.Int32(4),
		LogFile:        proto.String("restore.log"),
		TcpEstablished: proto.Bool(true),
	}

	return opts

}

// reach into DumpStorageDir and pick last modified folder
func getLatestCheckpointDir(dumpdir string) (string, error) {
	// potentially funny hack here instead is splitting the directory string
	// and getting the date out of that
	var dir string
	var lastModifiedTime time.Time

	folders, _ := os.ReadDir(dumpdir)
	for _, folder := range folders {
		fsinfo, err := folder.Info()
		if err != nil {
			return dir, err
		}
		if folder.IsDir() && fsinfo.ModTime().After(lastModifiedTime) {
			// see https://github.com/golang/go/issues/32300 about names
			// TL;DR - full name is only returned if the file is opened with the full name
			// TODO: audit code to find places where absolute paths _aren't_ used
			// hack: same as in prepareRestore, join folder.Name w/ dumpdir
			dir = filepath.Join(dumpdir, folder.Name())
			lastModifiedTime = fsinfo.ModTime()
		}
	}

	return dir, nil
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

func (c *Client) CUDARestore() {

}

func (c *Client) restore(cmd *ServerCommand, path *string) error {
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
		switch cmd.CedanaCheckpoint.CheckpointType {
		case CheckpointTypeCRIU:
			tmpdir, err := c.prepareManagedRestore(&opts, cmd)
			if err != nil {
				return err
			}
			dir = *tmpdir

			err = c.criuRestore(&opts, nfy, dir)
			if err != nil {
				return err
			}

		case CheckpointTypePytorch:
			err := c.pyTorchRestore()
			if err != nil {
				return err
			}
		}
	} else if path != nil {
		dir, err := c.prepareRestore(&opts, *path)
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
