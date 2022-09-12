package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/checkpoint-restore/go-criu"
	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/nravic/cedana-client/utils"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

var clientRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Initialize client and restore from dumped image",
	RunE: func(cmd *cobra.Command, args []string) error {
		// want to be able to get the criu object from the root, but that's neither here nor there
		c, err := instantiateClient()
		if err != nil {
			return err
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

	// automate
	cmd := exec.Command("mv", "code-server.log.bak", "code-server.log")
	cmd.Run()

	cmd = exec.Command("chmod", "664", "code-server.log")
	cmd.Run()

	// TODO: restore needs to do some work here (restoring connections?)
	err = c.CRIU.Restore(opts, criu.NoNotify{})
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error restoring process")
		return err

	}

	c.cleanupClient()

	return nil
}

func init() {
	clientCommand.AddCommand(clientRestoreCmd)
}
