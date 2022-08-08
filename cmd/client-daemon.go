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

		startDaemon(c)
		return nil
	},
}

// want to start a daemon!
func startDaemon(c *Client) chan struct{} {
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

	// start dumping loop
	// TODO - this should eventually be a function that takes event hooks
	ticker := time.NewTicker(10 * time.Minute)
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
