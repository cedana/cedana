package cmd

import (
	"log"

	"github.com/checkpoint-restore/go-criu"
	"github.com/spf13/cobra"
)

var clientCommand = &cobra.Command{
	Use:   "client",
	Short: "Use with dump or restore (dump first obviously)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func instantiate_client() (*criu.Criu, error) {
	c := criu.MakeCriu()
	// check if version is good, otherwise get out
	_, err := c.GetCriuVersion()
	if err != nil {
		log.Fatal("Error checking CRIU version!", err)
		return nil, err
	}

	// prepare client

	err = c.Prepare()
	if err != nil {
		log.Fatal("Error preparing CRIU client", err)
		return nil, err
	}

	// client server
	// 	_, err = http.Get("someUrl/init")
	// if err != nil {
	// log.Fatal("Could not init w/ server", err)
	// // do nothing for now here, want to be able to test
	// }

	// TODO: How to clean up client? Don't want to instatiate every time

	return c, nil
}

func init() {
	rootCmd.AddCommand(clientCommand)
}
