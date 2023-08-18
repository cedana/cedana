package cmd

import "github.com/spf13/cobra"

// cmd to grab logs if a job is running w/ socat enabled
var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Grab logs from a running job",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := instantiateClient()
		if err != nil {
			c.logger.Fatal().Err(err).Msg("Could not instantiate client")
		}

		// just pipe and read from fd1 lol

		return nil
	},
}

func init() {
	rootCmd.AddCommand(logCmd)
}
