package cmd

import (
	"time"

	"github.com/nravic/cedana-client/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

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

func (c *Client) startDaemon() chan struct{} {
	// on start, check w/ server w/ initializeClient
	// start pushing out state regularly to server
	// on intervals from config, dump
	registerRPCClient(*c.rpcClient)
	config, err := utils.InitConfig()
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error loading config")
	}

	// goroutine for a listener
	go runRecordState(*c.rpcClient)

	pid, err := utils.GetPid(viper.GetString("process_name"))
	if err != nil {
		c.logger.Fatal().Err(err).Msg("error getting process pid")
	}

	// when the config is statically typed, we won't be worried about getting a weird
	// var from this, because the act of initing config will error out
	dumping_frequency := config.Client.DumpFrequencyMin
	dump_storage_dir := config.Client.DumpStorageDir

	// start dumping loop
	// TODO - this should eventually be a function that takes event hooks
	ticker := time.NewTicker(time.Duration(dumping_frequency) * time.Minute)
	quit := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				// todo add incremental checkpointing
				err := c.dump(pid, dump_storage_dir)
				if err != nil {
					c.logger.Fatal().Err(err).Msg("error dumping process")
				}
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	return quit
}

func killDaemon(quit chan struct{}) {
	close(quit)
}

func init() {
	clientCommand.AddCommand(clientDaemonCmd)
}
