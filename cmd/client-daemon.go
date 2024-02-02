package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/utils"
	"github.com/spf13/cobra"
)

var clientDaemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start daemon for cedana client. Must be run as root, needed for all other cedana functionality.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("missing subcommand")
	},
}

var startDaemonCmd = &cobra.Command{
	Use:   "start",
	Short: "Starts the rpc server. To run as a daemon, use the provided script or use systemd/sysv/upstart.",
	Run: func(cmd *cobra.Command, args []string) {
		logger := utils.GetLogger()

		if os.Getenv("CEDANA_PROFILING_ENABLED") == "true" {
			logger.Info().Msg("profiling enabled, listening on 6060")
			go startProfiler()
		}

		logger.Info().Msgf("daemon started at %s", time.Now().Local())

		startgRPCServer()
	},
}

func startgRPCServer() {
	logger := utils.GetLogger()

	if _, err := api.StartGRPCServer(); err != nil {
		logger.Error().Err(err).Msg("Failed to start gRPC server")
	}

}

// Used for debugging and profiling only!
func startProfiler() {
	utils.StartPprofServer()
}

func init() {
	rootCmd.AddCommand(clientDaemonCmd)
	clientDaemonCmd.AddCommand(startDaemonCmd)
}
