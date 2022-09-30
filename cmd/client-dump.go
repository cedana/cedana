package cmd

import (
	"encoding/json"
	"os"
	"syscall"
	"time"

	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/nravic/cedana/utils"
	"github.com/shirou/gopsutil/v3/process"
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

func (c *Client) prepare_dump(pid int, dir string, opts *rpc.CriuOpts) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		c.logger.Fatal().Err(err).Msg("Could not instantiate new gopsutil process")
	}
	// check file descriptors
	open_files, err := p.OpenFiles()
	if err != nil {
		c.logger.Fatal().Err(err)
	}
	// marshal to dir folder for now
	b, _ := json.Marshal(open_files)
	c.logger.Debug().Msgf("pid has open fds: %s", string(b))
	// check network connections
	var hasTCP bool
	var hasExtUnixSocket bool
	conns, err := p.Connections()
	if err != nil {
		c.logger.Fatal().Err(err)
	}
	for _, conn := range conns {
		if conn.Type == syscall.SOCK_STREAM { // TCP
			hasTCP = true
		}

		if conn.Type == syscall.AF_UNIX { // interprocess
			hasExtUnixSocket = true
		}
	}
	opts.TcpEstablished = proto.Bool(hasTCP)
	opts.ExtUnixSk = proto.Bool(hasExtUnixSocket)
}

func (c *Client) prepare_opts() rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel:     proto.Int32(4),
		LogFile:      proto.String("dump.log"),
		ShellJob:     proto.Bool(false),
		LeaveRunning: proto.Bool(true),
		GhostLimit:   proto.Uint32(uint32(10000000)),
		ExtMasters:   proto.Bool(true),
	}
	return opts

}

func (c *Client) dump(pid int, dir string) error {
	defer c.timeTrack(time.Now(), "dump")

	// TODO - Dynamic storage (depending on process)
	img, err := os.Open(dir)
	if err != nil {
		c.logger.Fatal().Err(err).Msgf("could not open checkpoint storage dir %s", dir)
		return err
	}
	defer img.Close()

	// ideally we can load and unmarshal this entire struct, from a partial block in the config
	opts := c.prepare_opts()
	c.prepare_dump(pid, dir, &opts)

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))
	opts.Pid = proto.Int32(int32(pid))

	nfy := utils.Notify{
		Config:          c.config,
		Logger:          c.logger,
		PreDumpAvail:    true,
		PostDumpAvail:   true,
		PreRestoreAvail: true,
	}

	c.logger.Info().Msgf(`beginning dump of pid %d`, pid)

	err = c.CRIU.Dump(opts, nfy)
	if err != nil {
		// TODO - better error handling
		c.logger.Fatal().Err(err).Msg("error dumping process")
		return err
	}

	return nil
}
