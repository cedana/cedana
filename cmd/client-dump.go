package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
			dir = c.config.SharedStorage.DumpStorageDir
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

func (c *Client) prepareDump(pid int, dir string, opts *rpc.CriuOpts) string {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		c.logger.Fatal().Err(err).Msg("Could not instantiate new gopsutil process")
	}
	pname, err := utils.GetProcessName(pid)
	if err != nil {
		c.logger.Fatal().Err(err)
	}
	// check file descriptors
	open_files, err := p.OpenFiles()
	if err != nil {
		c.logger.Fatal().Err(err)
	}
	// marshal to dir folder for now
	b, _ := json.Marshal(open_files)
	c.logger.Debug().Msgf("pid has open fds: %s", string(b))

	// write to file for restore to use
	err = os.WriteFile("open_fds.json", b, 0o644)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error writing open_fds to disk")
	}

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

	// check tty state
	// if pts is in open fds, chances are it's a shell job
	var isShellJob bool
	for _, f := range open_files {
		if strings.Contains(f.Path, "pts") {
			isShellJob = true
			break
		}
	}
	opts.ShellJob = proto.Bool(isShellJob)

	// create subfolder to dump in
	// TODO: We're not taking advantage of incremental dumps with this

	// processname + datetime
	newdirname := strings.Join([]string{*pname, time.Now().Format("02_01_2006_1504")}, "_")
	dumpdir := filepath.Join(dir, newdirname)
	_, err = os.Stat(filepath.Join(dumpdir))
	if err != nil {
		if err := os.Mkdir(dumpdir, 0o755); err != nil {
			c.logger.Fatal().Err(err).Msg("error creating dump subfolder")
		}
	}

	return dumpdir
}

func (c *Client) postDump(dumpdir string) {
	c.logger.Info().Msg("compressing checkpoints")
	// if shared network storage is enabled, compress and shoot over
	if c.config.SharedStorage.MountPoint != "" {
		// dump onto mountpoint w/ folder name
		c.logger.Debug().Msgf("zipping into: %s", filepath.Join(
			c.config.SharedStorage.MountPoint, strings.Join(
				[]string{filepath.Base(dumpdir), ".tar.gz"}, "")))
		out, err := os.Create(
			filepath.Join(
				c.config.SharedStorage.MountPoint, strings.Join(
					[]string{filepath.Base(dumpdir), ".tar.gz"}, ""),
			),
		)
		if err != nil {
			c.logger.Fatal().Err(err).Msg("could not create compressed object")
		}
		err = utils.Compress(dumpdir, out)
		if err != nil {
			c.logger.Fatal().Err(err)
		}
	}
	// TODO: md5 checksum validation
}

func (c *Client) prepareCheckpointOpts() rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel:     proto.Int32(4),
		LogFile:      proto.String("dump.log"),
		LeaveRunning: proto.Bool(c.config.Client.LeaveRunning), // defaults to false
		GhostLimit:   proto.Uint32(uint32(10000000)),
		ExtMasters:   proto.Bool(true),
	}
	return opts

}

func (c *Client) dump(pid int, dir string) error {
	defer c.timeTrack(time.Now(), "dump")

	opts := c.prepareCheckpointOpts()
	dumpdir := c.prepareDump(pid, dir, &opts)

	img, err := os.Open(dumpdir)
	if err != nil {
		c.logger.Fatal().Err(err).Msgf("could not open checkpoint storage dir %s", dir)
		return err
	}
	defer img.Close()

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

	// CRIU ntfy hooks get run before this,
	// so have to ensure that image files aren't tampered with
	c.postDump(dumpdir)

	return nil
}
