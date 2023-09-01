package cmd

import (
	"flag"
	"fmt"
	"os"
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
		c, err := InstantiateClient()
		if err != nil {
			c.logger.Fatal().Err(err).Msg("Could not instantiate client")
		}

		err = c.AddDaemonLayer()
		if err != nil {
			c.logger.Fatal().Err(err).Msg("Could not add daemon layer")
		}

		c.Process.PID = pid

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
			LogFilePerm: 0o664,
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

		// create a subscription to NATS commands from the orchestrator first
		go c.subscribeToCommands(300)

		if pid == 0 {
			err := c.tryStartJob()
			// if we hit an error here, unrecoverable
			if err != nil {
				c.logger.Fatal().Err(err).Msg("could not start job")
			}
		}

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

func (c *Client) tryStartJob() error {
	var task string = c.config.Client.Task
	// 5 attempts arbitrarily chosen - up to the orchestrator to send the correct task
	var err error
	for i := 0; i < 5; i++ {
		pid, err := c.runTask(task)
		if err == nil {
			c.logger.Info().Msgf("managing process with pid %d", pid)
			c.state.Flag = cedana.JobRunning
			c.Process.PID = pid
			break
		} else {
			// enter a failure state, where we wait indefinitely for a command from NATS instead of
			// continuing
			c.logger.Info().Msgf("failed to run task with error: %v, attempt %d", err, i+1)
			c.state.Flag = cedana.JobStartupFailed
			recoveryCmd := c.enterDoomLoop()
			task = recoveryCmd.UpdatedTask
		}
	}

	if err != nil {
		return err
	}

	return nil
}

func (c *Client) runTask(task string) (int32, error) {
	var pid int32

	if task == "" {
		return 0, fmt.Errorf("could not find task in config")
	}

	// need a more resilient/retriable way of doing this
	r, w, err := os.Pipe()
	if err != nil {
		return 0, err
	}

	// An ampersand at the end of the task backgrounds it, making the shell
	// spawned by go to exit immediately and leave us with a defunct process.
	// Go then captures that defunct PID which is no good.
	// TODO NR - catch for ampersands? Or think of a better way of doing this.

	attr := &syscall.ProcAttr{
		Files: []uintptr{os.Stdin.Fd(), w.Fd(), w.Fd()}, // Stdin, Stdout, Stderr
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}

	argv := []string{"-c", task}
	p, err := syscall.ForkExec("/bin/sh", argv, attr)
	if err != nil {
		return 0, err
	}

	pid = int32(p)
	ppid := int32(os.Getpid())

	c.closeCommonFds(ppid, pid)

	go c.publishLogs(r)

	return pid, nil
}

func (c *Client) startDaemon(pid int32) {
LOOP:
	for {
		select {
		case <-c.channels.dump_command:
			c.logger.Info().Msg("received checkpoint command from NATS server")
			err := c.Dump(dir)
			if err != nil {
				// we don't want the daemon blowing up, so don't pass the error up
				c.logger.Warn().Msgf("could not checkpoint with error: %v", err)
				c.state.CheckpointState = cedana.CheckpointFailed
				c.publishStateOnce(c.getState(c.Process.PID))
			}
			c.state.CheckpointState = cedana.CheckpointSuccess
			c.publishStateOnce(c.getState(c.Process.PID))

		case cmd := <-c.channels.restore_command:
			// same here - want the restore to be retriable in the future, so makes sense not to blow it up
			c.logger.Info().Msg("received restore command from the NATS server")
			err := c.Restore(&cmd, nil)
			if err != nil {
				c.logger.Warn().Msgf("could not restore with error: %v", err)
				c.state.CheckpointState = cedana.RestoreFailed
				c.publishStateOnce(c.getState(c.Process.PID))
			}
			c.state.CheckpointState = cedana.RestoreSuccess
			c.publishStateOnce(c.getState(c.Process.PID))

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
