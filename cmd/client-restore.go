package cmd

import (
	"fmt"
	"os"

	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/nravic/cedana-client/utils"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

func init() {
	clientCommand.AddCommand(clientRestoreCmd)
	dumpCommand.Flags().StringVarP(&dir, "dumpdir", "d", "", "folder to restore checkpoint from")
}

var clientRestoreCmd = &cobra.Command{
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

func (c *Client) restore() error {
	config, err := utils.InitConfig()
	if err != nil {
		return fmt.Errorf("could not load config")
	}
	img, err := os.Open(config.Client.DumpStorageDir)
	if err != nil {
		return fmt.Errorf("can't open image dir")
	}
	defer img.Close()

	opts := rpc.CriuOpts{
		ImagesDirFd:    proto.Int32(int32(img.Fd())),
		LogLevel:       proto.Int32(4),
		LogFile:        proto.String("restore.log"),
		TcpEstablished: proto.Bool(true),
	}

	nfy := utils.Notify{
		Config:          c.config,
		Logger:          c.logger,
		PreDumpAvail:    true,
		PostDumpAvail:   true,
		PreRestoreAvail: true,
	}

	// automate
	// TODO: restore needs to do some work here (restoring connections?)
	err = c.CRIU.Restore(opts, nfy)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error restoring process")
		return err

	}

	c.cleanupClient()

	return nil
}
