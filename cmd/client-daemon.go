package cmd

import (
	"github.com/nravic/cedana/utils"
	"github.com/spf13/cobra"
)

func init() {
	clientCommand.AddCommand(clientDaemonCmd)
	clientDaemonCmd.Flags().IntVarP(&pid, "pid", "p", 0, "pid to dump")
}

var clientDaemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start daemon, and dump checkpoints to disk on a timer",
	RunE: func(cmd *cobra.Command, args []string) error {
		// want to be able to get the criu object from the root, but that's neither here nor there
		c, err := instantiateClient()
		if err != nil {
			return err
		}

		daemonChan := c.startDaemon()
		defer killDaemon(daemonChan)
		return nil
	},
}

func (c *Client) startDaemon() chan int {
	// start process checkpointing daemongo c.registerRPCClient()

	config, err := utils.InitConfig()
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error loading config")
	}

	if pid == 0 {
		pid, err = utils.GetPid(c.config.Client.ProcessName)
		if err != nil {
			c.logger.Err(err).Msg("Could not parse process name from config")
		}
	}

	dir := config.Client.DumpStorageDir

	go c.registerRPCClient(pid)

	// verify channels exist to listen on
	if c.channels == nil {
		c.logger.Fatal().Msg("Dump and restore channels uninitialized!")
	}

	quit := make(chan int)

	// start goroutines
	go c.pollForCommand(pid)

	go func() {
		for {
			select {
			case <-c.channels.dump_command:
				// todo add incremental checkpointing
				err := c.dump(pid, dir)
				if err != nil {
					c.logger.Fatal().Err(err).Msg("error dumping process")
				}
			case <-c.channels.restore_command:
				err := c.restore()
				if err != nil {
					c.logger.Fatal().Err(err).Msg("error restoring process")
				}
			case <-quit:
				return
			}
		}
	}()

	return quit
}

func killDaemon(quit chan int) {
	close(quit)
}
