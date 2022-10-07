package cmd

import (
	"flag"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/nravic/cedana/utils"
	gd "github.com/sevlyar/go-daemon"
	"github.com/spf13/cobra"
)

func init() {
	clientCommand.AddCommand(clientDaemonCmd)
	clientDaemonCmd.Flags().IntVarP(&pid, "pid", "p", 0, "pid to dump")
}

var stop = make(chan struct{})
var done = make(chan struct{})
var signal = flag.String("s", "", "")

var clientDaemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start daemon, and dump checkpoints to disk as commanded by an orchestrator",
	Run: func(cmd *cobra.Command, args []string) {
		c, err := instantiateClient()
		if err != nil {
			c.logger.Fatal().Err(err).Msg("Could not instantiate client")
		}

		if pid == 0 {
			pid, err = utils.GetPid(c.config.Client.ProcessName)
			if err != nil {
				c.logger.Err(err).Msg("Could not parse process name from config")
			}
		}

		if dir == "" {
			dir = c.config.Client.DumpStorageDir
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
			PidFilePerm: 0644,
			LogFileName: "cedana-daemon.log",
			LogFilePerm: 0640,
			WorkDir:     "./",
			Umask:       027,
			Args:        []string{executable, "client", "daemon", "-p", fmt.Sprint(pid)},
		}

		gd.AddCommand(gd.StringFlag(signal, "stop"), syscall.SIGTERM, termHandler)

		d, err := ctx.Reborn()
		if err != nil {
			c.logger.Err(err).Msg("could not start daemon")
		}

		if d != nil {
			return
		}

		defer ctx.Release()

		c.logger.Info().Msg("--------------------------------")
		c.logger.Info().Msg("daemon started")

		c.registerRPCClient(pid)

		// start polling orchestrator for work, in one goroutine
		go c.pollForCommand(pid)

		// start daemon worker in another
		go c.startDaemon()

		err = gd.ServeSignals()
		if err != nil {
			c.logger.Fatal().Err(err)
		}

		c.logger.Info().Msg("daemon terminated")
	},
}

func (c *Client) startDaemon() {
LOOP:
	for {
		time.Sleep(time.Second)
		select {
		case <-c.channels.dump_command:
			err := c.dump(pid, dir)
			if err != nil {
				c.logger.Fatal().Err(err).Msg("error dumping process")
			}
		case <-c.channels.restore_command:
			err := c.restore()
			if err != nil {
				c.logger.Fatal().Err(err).Msg("error restoring process")
			}
		case <-stop:
			c.logger.Info().Msg("stop hit")
			break LOOP
		default:
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
