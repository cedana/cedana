package cmd

import (
	"encoding/json"
	"fmt"
	"os"
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
		// want to be able to get the criu object from the root, but that's neither here nor there
		c, err := instantiateClient()
		if err != nil {
			return err
		}

		if dir == "" {
			dir = c.config.Client.DumpStorageDir
		}

		err = c.restore()
		if err != nil {
			return err
		}
		return nil
	},
}

func (c *Client) prepareRestore(opts *rpc.CriuOpts) {
	// check open_fds. Useful for checking if process being restored
	// is a pts slave and for determining how to handle files that were being written to.
	// TODO: We should be looking at the images instead
	var open_fds []process.OpenFilesStat
	var isShellJob bool
	data, err := os.ReadFile("open_fds.json")
	if err != nil {
		c.logger.Fatal().Err(err).Msg("could not read open_fds json")
	}
	err = json.Unmarshal(data, &open_fds)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("could not unmarshal open_fds to []process.OpenFilesStat")
	}

	for _, f := range open_fds {
		if strings.Contains(f.Path, "pts") {
			isShellJob = true
			break
		}
	}

	opts.ShellJob = proto.Bool(isShellJob)

	// TODO: network restore logic

}

func (c *Client) prepareRestoreOpts(img *os.File) rpc.CriuOpts {
	opts := rpc.CriuOpts{
		ImagesDirFd:    proto.Int32(int32(img.Fd())),
		LogLevel:       proto.Int32(4),
		LogFile:        proto.String("restore.log"),
		TcpEstablished: proto.Bool(true),
	}

	return opts

}

func (c *Client) restore() error {
	defer c.timeTrack(time.Now(), "restore")
	config, err := utils.InitConfig()
	if err != nil {
		return fmt.Errorf("could not load config")
	}
	img, err := os.Open(config.Client.DumpStorageDir)
	if err != nil {
		return fmt.Errorf("can't open image dir")
	}
	defer img.Close()

	opts := c.prepareRestoreOpts(img)
	c.prepareRestore(&opts)

	nfy := utils.Notify{
		Config:          c.config,
		Logger:          c.logger,
		PreDumpAvail:    true,
		PostDumpAvail:   true,
		PreRestoreAvail: true,
	}

	err = c.CRIU.Restore(opts, nfy)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error restoring process")
		return err

	}

	c.cleanupClient()

	return nil
}
