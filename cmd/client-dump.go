package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// used for pid comms
const (
	sys_pidfd_send_signal = 424
	sys_pidfd_open        = 434
	sys_pidfd_getfd       = 438
)

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

		c.process.Pid = pid

		err = c.dump(dir)
		if err != nil {
			return err
		}

		defer c.cleanupClient()
		return nil
	},
}

// Signals a process prior to dumping with SIGUSR1
func (c *Client) signalProcessAndWait(pid int, timeout int) {
	fd, _, err := syscall.Syscall(sys_pidfd_open, uintptr(pid), 0, 0)
	if err != 0 {
		c.logger.Fatal().Err(err).Msg("could not open pid")
	}
	s := syscall.SIGUSR1
	_, _, err = syscall.Syscall6(sys_pidfd_send_signal, uintptr(fd), uintptr(s), 0, 0, 0, 0)
	if err != 0 {
		c.logger.Info().Msgf("could not send signal to pid %s", pid)
	}

	// we want to sleep the dumping thread here to wait for the process
	// to finish executing. This likely won't have any effects when run in daemon mode,
	// it'll just pause the spawned dump goroutine
	time.Sleep(time.Duration(timeout) * time.Second)
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

	// check for GPU & send a simple signal if active
	// this is hacky, but more often than not a sign that we've got a GPU
	// TODO: only checks nvidia?
	var attachedToHardwareAccel bool
	var gpuFds []process.OpenFilesStat
	for _, f := range open_files {
		if strings.Contains(f.Path, "nvidia") {
			gpuFds = append(gpuFds, f)
		}
	}
	if len(gpuFds) != 0 {
		attachedToHardwareAccel = true
	}
	// if the user hasn't configured signaling in the case they're using the GPU
	// they haven't read the docs and the signal just gets lost in the aether anyway
	if attachedToHardwareAccel && c.config.Client.SignalProcessPreDump {
		c.logger.Info().Msgf("GPU use detected, signaling process pid %d and waiting for %d s...", pid, c.config.Client.SignalProcessTimeout)
		// for now, don't set any opts and skip using CRIU. Future work to integrate Cricket and intercept CUDA calls

		c.process.AttachedToHardwareAccel = attachedToHardwareAccel
		c.signalProcessAndWait(pid, c.config.Client.SignalProcessTimeout)
	}

	// processname + datetime
	// strip out non posix-compliant characters from the processname
	formattedProcessName := regexp.MustCompile("[^a-zA-Z0-9_.-]").ReplaceAllString(*pname, "_")
	formattedProcessName = strings.ReplaceAll(formattedProcessName, ".", "_")
	newdirname := strings.Join([]string{formattedProcessName, time.Now().Format("02_01_2006_1504")}, "_")
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
	// HACK! just copy the pytorch checkpoint into the dumpdir - make this better
	if c.process.AttachedToHardwareAccel {
		// find latest pt file in dump_storage_dir
		var pt string
		latestModTime := time.Time{}
		err := filepath.Walk(c.config.SharedStorage.DumpStorageDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// pytorch checkpoints only
			if !info.IsDir() && filepath.Ext(path) == ".pt" {
				modTime := info.ModTime()
				if modTime.After(latestModTime) {
					pt = path
					latestModTime = modTime
				}
			}
			return nil
		})
		if err != nil {
			fmt.Println(err)
			return
		}
		// TODO: add other application/GPU-level checkpoints
		if pt == "" {
			c.logger.Debug().Msg("could not find a pytorch checkpoint")
		} else {
			c.logger.Debug().Msgf("found checkpoint, moving to %s", dumpdir)
			// copy to dumpdir
			data, err := os.ReadFile(pt)
			if err != nil {
				// drop
				return
			}
			err = os.WriteFile(strings.Join([]string{dumpdir, filepath.Base(pt)}, "/"), data, 0o644)
			if err != nil {
				c.logger.Debug().Msgf("could not move checkpoint %v", err)
				return
			}
		}
	}
	if c.config.SharedStorage.MountPoint != "" {
		// dump onto mountpoint w/ folder name
		c.logger.Debug().Msgf("zipping into: %s", filepath.Join(
			c.config.SharedStorage.MountPoint, strings.Join(
				[]string{filepath.Base(dumpdir), ".zip"}, "")))
		out := filepath.Join(
			c.config.SharedStorage.MountPoint, strings.Join(
				[]string{filepath.Base(dumpdir), ".zip"}, ""))

		err := utils.CompressFolder(dumpdir, out)
		if err != nil {
			c.logger.Fatal().Err(err)
		}
		if c.config.CedanaManaged {
			c.logger.Info().Msg("client is managed by a cedana orchestrator, pushing checkpoint..")
			err := c.publishCheckpointFile(out)
			if err != nil {
				c.logger.Info().Msgf("error pushing checkpoint: %v", err)
			}
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

func (c *Client) dump(dir string) error {
	defer c.timeTrack(time.Now(), "dump")

	opts := c.prepareCheckpointOpts()
	dumpdir := c.prepareDump(c.process.Pid, dir, &opts)

	img, err := os.Open(dumpdir)
	if err != nil {
		c.logger.Fatal().Err(err).Msgf("could not open checkpoint storage dir %s", dir)
		return err
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))
	opts.Pid = proto.Int32(int32(c.process.Pid))

	nfy := utils.Notify{
		Config:          c.config,
		Logger:          c.logger,
		PreDumpAvail:    true,
		PostDumpAvail:   true,
		PreRestoreAvail: true,
	}

	c.logger.Info().Msgf(`beginning dump of pid %d`, c.process.Pid)

	if !c.process.AttachedToHardwareAccel {
		err = c.CRIU.Dump(opts, nfy)
		if err != nil {
			// TODO - better error handling
			c.logger.Fatal().Err(err).Msg("error dumping process")
			return err
		}
	}

	// CRIU ntfy hooks get run before this,
	// so have to ensure that image files aren't tampered with
	c.postDump(dumpdir)
	c.cleanupClient()

	return nil
}
