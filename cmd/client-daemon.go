package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	gd "github.com/sevlyar/go-daemon"
	"github.com/spf13/cobra"

	cedana "github.com/cedana/cedana/types"
)

func init() {
	clientCommand.AddCommand(clientDaemonCmd)
	clientDaemonCmd.Flags().Int32VarP(&pid, "pid", "p", 0, "pid to manage")
}

var stop = make(chan struct{})
var done = make(chan struct{})
var daemonSignal = flag.String("s", "", "")

var clientDaemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start daemon, and dump checkpoints to disk as commanded by an orchestrator",
	Run: func(cmd *cobra.Command, args []string) {
		c, err := instantiateClient()
		if err != nil {
			c.logger.Fatal().Err(err).Msg("Could not instantiate client")
		}

		err = c.AddDaemonLayer()
		if err != nil {
			c.logger.Fatal().Err(err).Msg("Could not add daemon layer")
		}

		if pid == 0 {
			// we're running as a cedana-managed daemon, so run the task
			pid, err = c.startJob()
			if err != nil {
				c.logger.Fatal().Err(err).Msg("Could not start job!")
				// TODO NR - integrate this with NATS and messaging so we can retry if needed
			}
			c.logger.Info().Msgf("managing process with pid %d", pid)
		}

		c.process.PID = pid

		if dir == "" {
			dir = c.config.SharedStorage.DumpStorageDir
		}

		// verify channels exist to listen on
		if c.channels == nil {
			c.logger.Fatal().Msg("Dump and restore channels uninitialized!")
		}

		executable, err := os.Executable()
		if err != nil {
			c.logger.Fatal().Msg("Could not find cedana executable")
		}

		ctx := &gd.Context{
			PidFileName: "cedana.pid",
			PidFilePerm: 0o644,
			LogFileName: "cedana-daemon.log",
			LogFilePerm: 0o640,
			WorkDir:     "./",
			Umask:       027,
			Args:        []string{executable, "client", "daemon", "-p", fmt.Sprint(pid)},
		}

		gd.AddCommand(gd.StringFlag(daemonSignal, "stop"), syscall.SIGTERM, termHandler)

		d, err := ctx.Reborn()
		if err != nil {
			c.logger.Err(err).Msg("could not start daemon")
		}

		if d != nil {
			return
		}

		defer ctx.Release()

		c.logger.Info().Msgf("daemon started at %s", time.Now().Local())

		go c.subscribeToCommands(300)
		go c.publishStateContinuous(30)

		// start daemon worker
		go c.startDaemon(pid)

		err = gd.ServeSignals()
		if err != nil {
			c.logger.Fatal().Err(err)
		}

		c.logger.Info().Msg("daemon terminated")
	},
}

func (c *Client) startJob() (int32, error) {
	var pid int32

	task := c.config.Client.Task
	if task == "" {
		return 0, fmt.Errorf("Could not find task in config, something went wrong during setup!")
	}

	// validation of the task happens at the server level, so there's no real concerns here about
	// executing the task without proper validation

	// we want the task to run detached so we can checkpoint it
	cmd := exec.Command("/bin/sh", "-c", task)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Start(); err != nil {
		return 0, err
	}

	pid = int32(cmd.Process.Pid)

	return pid, nil
}

func (c *Client) startDaemon(pid int32) {
LOOP:
	for {
		select {
		case <-c.channels.dump_command:
			c.logger.Info().Msg("received checkpoint command from NATS server")
			err := c.dump(dir)
			if err != nil {
				// we don't want the daemon blowing up, so don't pass the error up
				c.logger.Warn().Msgf("could not checkpoint with error: %v", err)
				c.state.CheckpointState = cedana.CheckpointFailed
				c.publishStateOnce()
			}
			c.state.CheckpointState = cedana.CheckpointSuccess
			c.publishStateOnce()

		case cmd := <-c.channels.restore_command:
			// same here - want the restore to be retriable in the future, so makes sense not to blow it up
			c.logger.Info().Msg("received restore command from the NATS server")
			err := c.restore(&cmd, nil)
			if err != nil {
				c.logger.Warn().Msgf("could not restore with error: %v", err)
				c.state.CheckpointState = cedana.RestoreFailed
				c.publishStateOnce()
			}
			c.state.CheckpointState = cedana.RestoreSuccess
			c.publishStateOnce()

		case <-stop:
			c.logger.Info().Msg("stop hit")
			break LOOP

		default:
			time.Sleep(1 * time.Second)
		}
	}
}

func termHandler(sig os.Signal) error {
	stop <- struct{}{}
	if sig == syscall.SIGQUIT {
		<-done
	}
	return gd.ErrStop
}
