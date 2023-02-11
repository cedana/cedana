package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/nravic/cedana/utils"
	"github.com/shirou/gopsutil/process"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

func init() {
	clientCommand.AddCommand(restoreCmd)
	restoreCmd.Flags().StringVarP(&dir, "dumpdir", "d", "", "folder to restore checkpoint from")
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Initialize client and restore from dumped image",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := instantiateClient()
		if err != nil {
			return err
		}

		if dir == "" {
			dir = c.config.SharedStorage.DumpStorageDir
		}

		err = c.restore()
		if err != nil {
			return err
		}
		return nil
	},
}

func (c *Client) prepareRestore(opts *rpc.CriuOpts, dumpdir string) {
	// check open_fds. Useful for checking if process being restored
	// is a pts slave and for determining how to handle files that were being written to.
	// TODO: We should be looking at the images instead
	var open_fds []process.OpenFilesStat
	var isShellJob bool
	data, err := os.ReadFile("open_fds.json")
	if err == nil {
		err = json.Unmarshal(data, &open_fds)
		if err != nil {
			// we don't really care if we can't read this file
			c.logger.Info().Err(err).Msg("could not unmarshal open_fds to []process.OpenFilesStat")
		}

		for _, f := range open_fds {
			if strings.Contains(f.Path, "pts") {
				isShellJob = true
				break
			}
		}
	}
	opts.ShellJob = proto.Bool(isShellJob)

	// TODO: network restore logic

	// if shared network storage is enabled, grab last modified object
	if c.config.SharedStorage.MountPoint != "" {
		c.logger.Info().Msg("decompressing checkpoints")
		var f string
		var lastModifiedTime time.Time

		files, _ := os.ReadDir(c.config.SharedStorage.MountPoint)
		for _, file := range files {
			fsinfo, err := file.Info()
			if err != nil {
				c.logger.Fatal().Err(err)
			}
			if fsinfo.ModTime().After(lastModifiedTime) && filepath.Ext(fsinfo.Name()) == "zip" {
				f = file.Name()
				lastModifiedTime = fsinfo.ModTime()
			}
		}

		// hack: fsInfo for whatever reason doesn't expose the absolute path. Thankfully we have enough
		// information to piece it together, but this feels brittle.
		f = filepath.Join(c.config.SharedStorage.MountPoint, f)
		c.logger.Info().Msgf("found cedana checkpoint %s", f)
		// from a shared volume (like efs) -> local storage
		c.logger.Info().Msgf("decompressing %s to %s", f, dumpdir)
		err = utils.DecompressFolder(f, dumpdir)
		if err != nil {
			// hack: error here is not fatal due to EOF (from a badly written utils.Compress)
			c.logger.Info().Err(err).Msg("error decompressing checkpoint")
		}
	}

	// clean up open fds
	err = os.Remove("open_fds.json")
	if err != nil {
		c.logger.Info().Msgf("error %v deleting openfds file", err)
	}
	// TODO: md5 checksum validation
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

func (c *Client) restore() error {
	defer c.timeTrack(time.Now(), "restore")

	opts := c.prepareRestoreOpts()
	c.prepareRestore(&opts, c.config.SharedStorage.DumpStorageDir)

	nfy := utils.Notify{
		Config:          c.config,
		Logger:          c.logger,
		PreDumpAvail:    true,
		PostDumpAvail:   true,
		PreRestoreAvail: true,
	}

	// even if external storage is used, checkpoints dumped by cedana will be in the format
	// /dumpdir/processname_date/...
	dir, err := getLatestCheckpointDir(c.config.SharedStorage.DumpStorageDir)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("could not get latest checkpoint directory")
	}

	img, err := os.Open(dir)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("could not open directory")
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))

	err = c.CRIU.Restore(opts, nfy)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error restoring process")
		return err
	}

	c.cleanupClient()

	return nil
}
