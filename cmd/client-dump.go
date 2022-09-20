package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/nravic/cedana/utils"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

var dir string
var pid int

func init() {
	clientCommand.AddCommand(dumpCommand)
	dumpCommand.Flags().StringVarP(&dir, "dir", "d", "", "folder to dump checkpoint into")
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
		// load from config if flags aren't set
		if dir == "" {
			dir = c.config.Client.DumpStorageDir
		}

		if pid == 0 {
			pid, err = utils.GetPid(c.config.Client.ProcessName)
			if err != nil {
				c.logger.Err(err).Msg("Could not parse process name from config")
				return err
			}
		}

		err = c.dump(pid, dir)
		if err != nil {
			return err
		}

		defer c.cleanupClient()
		return nil
	},
}

func (c *Client) prepare_dump(pid int, dir string) {
	// copy all open file descriptors for a process
	cmd := exec.Command("ls", "-l", "/proc/"+strconv.Itoa(pid)+"/fd")
	out, err := cmd.CombinedOutput()
	if err != nil {
		c.logger.Fatal().Err(err).Msgf(`could not ls /proc for pid %d`, pid)
	}
	c.logger.Debug().Bytes(fmt.Sprintf(`open fds for pid %d`, pid), out)
	err = os.WriteFile(fmt.Sprintf(`%s/open_fds`, dir), out, 0644)
}

func (c *Client) prepare_opts() rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel:       proto.Int32(4),
		LogFile:        proto.String("dump.log"),
		ShellJob:       proto.Bool(false),
		LeaveRunning:   proto.Bool(true),
		TcpEstablished: proto.Bool(true),
		GhostLimit:     proto.Uint32(uint32(10000000)),
		ExtMasters:     proto.Bool(true),
	}
	return opts

}

func (c *Client) dump(pid int, dir string) error {

	// TODO - Dynamic storage (depending on process)
	img, err := os.Open(dir)
	if err != nil {
		c.logger.Fatal().Err(err).Msgf("could not open checkpoint storage dir %s", dir)
		return err
	}
	defer img.Close()

	// ideally we can load and unmarshal this entire struct, from a partial block in the config
	c.prepare_dump(pid, dir)
	opts := c.prepare_opts()
	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))
	opts.Pid = proto.Int32(int32(pid))

	nfy := utils.Notify{
		Config:          c.config,
		Logger:          c.logger,
		PreDumpAvail:    true,
		PostDumpAvail:   true,
		PreRestoreAvail: true,
	}

	c.logger.Debug().Msgf("starting dump with opts: %+v\n", opts)

	// perform multiple consecutive passes of the dump, altering opts as needed
	// go-CRIU doesn't expose some of this stuff, need to hand-code
	// incrementally add as you test different processes and they fail

	c.logger.Info().Msgf(`beginning dump of pid %d`, pid)
	err = c.CRIU.Dump(opts, nfy)
	if err != nil {
		// TODO - better error handling
		c.logger.Fatal().Err(err).Msg("error dumping process")
		return err
	}

	return nil
}
