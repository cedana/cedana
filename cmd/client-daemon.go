package cmd

import (
	"log"
	"strconv"
	"time"

	"github.com/nravic/oort/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var clientDaemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Initialize client, start listening",
	RunE: func(cmd *cobra.Command, args []string) error {
		// want to be able to get the criu object from the root, but that's neither here nor there
		c, err := instantiateClient()
		if err != nil {
			return err
		}

		c.startDaemon()
		return nil
	},
}

func (c *Client) startDaemon() chan struct{} {
	// on start, check w/ server w/ initializeClient
	// start pushing out state regularly to server
	// on intervals from config, dump
	registerRPCClient(*c.rpcClient)
	utils.InitConfig()

	// goroutine for a listener
	go runRecordState(*c.rpcClient)

	pid, err := utils.GetPid(viper.GetString("process_name"))
	if err != nil {
		log.Fatal("Error getting process pid", err)
	}

	// when the config is statically typed, we won't be worried about getting a weird
	// var from this, because the act of initing config will error out
	dumping_frequency := viper.GetInt("dumping_frequency_min")

	// start dumping loop
	// TODO - this should eventually be a function that takes event hooks
	ticker := time.NewTicker(time.Duration(dumping_frequency) * time.Minute)
	quit := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				err := c.dump(strconv.Itoa(pid))
				if err != nil {
					// don't throw, just log
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
