package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/checkpoint-restore/go-criu"
	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

var clientRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Initialize client and restore from dumped image",
	RunE: func(cmd *cobra.Command, args []string) error {
		// want to be able to get the criu object from the root, but that's neither here nor there

		return nil
	},
}

func restore(c *criu.Criu) error {
	img, err := os.Open("~/dump_images")
	if err != nil {
		return fmt.Errorf("Can't open image dir")
	}
	defer img.Close()

	opts := rpc.CriuOpts{
		ImagesDirFd: proto.Int32(int32(img.Fd())),
		LogLevel:    proto.Int32(4),
		LogFile:     proto.String("dump.log"),
	}

	err := c.Restore(opts, criu.NoNotify{})
	if err != nil {
		log.Fatal("Error restoring process!", err)
		return err
	}

	c.Cleanup()

	return nil
}

func init() {
	clientCommand.AddCommand(clientRestoreCmd)
}
