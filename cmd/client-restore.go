package cmd

import (
	"log"

	"github.com/checkpoint-restore/go-criu"
	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

var clientRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "restore from dumped image",
	RunE: func(cmd *cobra.Command, args []string) error {

		return nil
	},
}

func restore(c *criu.Criu) error {
	opts := rpc.CriuOpts{
		Pid:      proto.Int32(int32(1000)),
		LogLevel: proto.Int32(4),
		LogFile:  proto.String("dump.log"),
	}

	err := c.Restore(opts, criu.NoNotify{})
	if err != nil {
		log.Fatal("Error restoring process!", err)
		return err
	}

	return nil
}

func init() {
	clientCommand.AddCommand()
}
