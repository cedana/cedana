package cmd

import (
	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/rs/zerolog"
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
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		root, _ := cmd.Flags().GetString(rootFlag)
		name := args[0]
		query := &task.RuncQueryArgs{
			Root:           root,
			ContainerNames: []string{name},
		}

		// FIXME YA: When no PID given, still returns something
		resp, err := cts.RuncQuery(ctx, query)
		if err != nil {
			logger.Error().Err(err).Msgf("Container \"%s\" not found", name)
			return
		}
		logger.Info().Msgf("Response: %v", resp.Containers[0])
	},
}

func init() {
	runcGetRuncIdByName.Flags().StringP(rootFlag, "r", RuncRootDir, "runc root directory")
	runcCmd.AddCommand(runcGetRuncIdByName)

	rootCmd.AddCommand(runcCmd)
}
