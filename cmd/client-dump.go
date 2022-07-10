package cmd

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/checkpoint-restore/go-criu"
	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

type StateHandlerRequest struct {
	State string `valid:"state"`
}

type StateHandlerResponse struct {
	State       string `valid:"state"`
	Instruction string `valid:"instruction"`
}

func init() {
	clientCommand.AddCommand(dumpCommand)
}

var dumpCommand = &cobra.Command{
	Use:   "dump",
	Short: "Initialize Client and dump a PID",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := instantiateClient()
		if err != nil {
			return err
		}
		err = c.dump(args[0])
		if err != nil {
			return err
		}

		c.cleanupClient()
		return nil
	},
}

func (c *Client) dump(pidS string) error {
	pid, err := strconv.ParseInt(pidS, 10, 0)
	if err != nil {
		return fmt.Errorf("can't parse pid: %w", err)
	}

	// TODO - Configurable storage location
	// TODO - Dynamic storage (depending on process)
	img, err := os.Open("/home/nravic/dump_images")
	if err != nil {
		return fmt.Errorf("can't open image dir: %v", err)
	}
	defer img.Close()

	opts := rpc.CriuOpts{
		// TODO: need to annotate this stuff, load from server on boot
		Pid:          proto.Int32(int32(pid)),
		LogLevel:     proto.Int32(1),
		LogFile:      proto.String("dump.log"),
		ImagesDirFd:  proto.Int32(int32(img.Fd())),
		ExtMasters:   proto.Bool(true),
		ShellJob:     proto.Bool(true),
		ExtUnixSk:    proto.Bool(true),
		LeaveRunning: proto.Bool(true),
	}

	err = c.CRIU.Dump(opts, criu.NoNotify{})
	if err != nil {
		log.Fatal("Error dumping process: ", err)
		return err
	}

	return nil
}
