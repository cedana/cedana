package cmd

import (
	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/utils"
	"github.com/spf13/cobra"
)

// Here I am introducing a new command to run the daemon in the background with a grpc server
var clientDaemonRPCCmd = &cobra.Command{
	Use:   "daemon-grpc",
	Short: "Start daemon for cedana client. Must be run as root, needed for all other cedana functionality.",
	Run: func(cmd *cobra.Command, args []string) {

		logger := utils.GetLogger()

		if err := api.StartGRPCServer(); err != nil {
			logger.Error().Err(err).Msg("Failed to start gRPC server")
		}

	},
}

func init() {
	rootCmd.AddCommand(clientDaemonRPCCmd)
}
