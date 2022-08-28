package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/checkpoint-restore/go-criu"
	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/nravic/oort/utils"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

var dump_storage_dir string
var pid int

func init() {
	clientCommand.AddCommand(dumpCommand)
	dumpCommand.Flags().StringVarP(&dump_storage_dir, "dumpdir", "d", "", "folder to dump checkpoint into")
	dumpCommand.Flags().IntVarP(&pid, "pid", "p", 0, "pid to dump")
}

// This is a direct dump command. Won't be used in practice, we want to start a daemon
var dumpCommand = &cobra.Command{
	Use:   "dump",
	Short: "Directly dump a process",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := instantiateClient()
		if err != nil {
			return err
		}
		config, err := utils.InitConfig()
		if err != nil {
			log.Fatal("Could not read config", err)
		}
		// load from config if flags aren't set
		if dump_storage_dir == "" {
			dump_storage_dir = config.Client.DumpStorageDir
		}

		if pid == 0 {
			pid, err = utils.GetPid(config.Client.ProcessName)
			if err != nil {
				return fmt.Errorf("could not parse process name from config")
			}
		}

		err = c.dump(pid, dump_storage_dir)
		if err != nil {
			return err
		}

		defer c.cleanupClient()
		return nil
	},
}

func (c *Client) dump(pid int, dump_storage_dir string) error {

	// TODO - Dynamic storage (depending on process)
	img, err := os.Open(dump_storage_dir)
	if err != nil {
		return fmt.Errorf("can't open image dir: %v", err)
	}
	defer img.Close()

	// ideally we can load and unmarshal this entire struct, from a partial block in the config

	opts := rpc.CriuOpts{
		// TODO: need to annotate this stuff, load from server on boot
		Pid:            proto.Int32(int32(pid)),
		LogLevel:       proto.Int32(1),
		LogFile:        proto.String("dump.log"),
		ImagesDirFd:    proto.Int32(int32(img.Fd())),
		ExtMasters:     proto.Bool(true),
		ShellJob:       proto.Bool(true),
		ExtUnixSk:      proto.Bool(true),
		LeaveRunning:   proto.Bool(true),
		TcpEstablished: proto.Bool(true),
		GhostLimit:     proto.Uint32(uint32(10000000)),
	}

	err = c.CRIU.Dump(opts, criu.NoNotify{})
	if err != nil {
		// TODO - better error handling
		log.Fatal("Error dumping process: ", err)
		return err
	}

	return nil
}
