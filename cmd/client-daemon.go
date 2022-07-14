package cmd

import "github.com/spf13/cobra"

var clientDaemonCmd = &cobra.Command{
	Use:   "restore",
	Short: "Initialize client and restore from dumped image",
	RunE: func(cmd *cobra.Command, args []string) error {
		// want to be able to get the criu object from the root, but that's neither here nor there
		c, err := instantiateClient()
		if err != nil {
			return err
		}
		err = restore(c.CRIU)
		if err != nil {
			return err
		}
		startDaemon()
		return nil
	},
}

// want to start a daemon!
func startDaemon() {
	// on start, check w/ server w/ initializeClient 
	// start pushing out state regularly to server 
	// on intervals from config, dump 

}

func init() {
	clientCommand.AddCommand(clientDaemonCmd)
}
