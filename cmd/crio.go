package cmd

import (
	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

var dumpCRIORootfs = &cobra.Command{
	Use:   "CRIORootfs",
	Short: "Manually commit a CRIO container",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		id, err := cmd.Flags().GetString(idFlag)
		if err != nil {
			logger.Error().Msgf("Error getting container id: %v", err)
		}

		dumpArgs := task.CRIORootfsDumpArgs{
			ContainerID: id,
		}

		resp, err := cts.CRIORootfsDump(ctx, &dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Checkpoint task failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
			return err
		}
		logger.Info().Msgf("Response: %v", resp.ImageRef)

		return nil
	},
}
