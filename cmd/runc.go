package cmd

import (
	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/spf13/cobra"
)

const RuncRootDir = "/var/run/runc"

var runcCmd = &cobra.Command{
	Use:   "runc",
	Short: "Runc container related commands",
}

var runcGetRuncIdByName = &cobra.Command{
	Use:   "get",
	Short: "Get runc id by container name",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cts, err := services.NewClient(api.Address)
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		root, _ := cmd.Flags().GetString(rootFlag)
		name := args[0]
		runcArgs := &task.CtrByNameArgs{
			Root:          root,
			ContainerName: name,
		}

		// FIXME YA: When no PID given, still returns something
		resp, err := cts.GetRuncIdByName(runcArgs)
		if err != nil {
			logger.Error().Err(err).Msgf("Container \"%s\" not found", name)
		} else {
			logger.Info().Msgf("Response: %v", resp)
		}
	},
}

func init() {
	runcGetRuncIdByName.Flags().StringP(rootFlag, "r", RuncRootDir, "runc root directory")
	runcCmd.AddCommand(runcGetRuncIdByName)

	rootCmd.AddCommand(runcCmd)
}
