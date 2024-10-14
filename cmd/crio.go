package cmd

import (
	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

var pushCRIOImage = &cobra.Command{
	Use:   "crio-push",
	Short: "Manually push a crio image",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cts := cmd.Context().Value(utils.CtsKey).(*services.ServiceClient)

		originalImageRef, err := cmd.Flags().GetString(refFlag)
		if err != nil {
			log.Error().Msgf("Error getting container id: %v", err)
		}
		newImageRef, err := cmd.Flags().GetString(newRefFlag)
		if err != nil {
			log.Error().Msgf("Error getting destination path: %v", err)
		}
		rootfsDiffPath, err := cmd.Flags().GetString(rootfsDiffPathFlag)
		if err != nil {
			log.Error().Msgf("Error getting container storage path: %v", err)
		}

		pushArgs := task.CRIOImagePushArgs{
			OriginalImageRef: originalImageRef,
			NewImageRef:      newImageRef,
			RootfsDiffPath:   rootfsDiffPath,
		}

		resp, err := cts.CRIOImagePush(ctx, &pushArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Msgf("Checkpoint task failed: %v, %v", st.Message(), st.Code())
			} else {
				log.Error().Msgf("Checkpoint task failed: %v", err)
			}
			return err
		}
		log.Info().Msgf("Response: %v", resp)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(pushCRIOImage)
	pushCRIOImage.Flags().StringP(refFlag, "", "", "original ref")
	pushCRIOImage.MarkFlagRequired(refFlag)
	pushCRIOImage.Flags().StringP(newRefFlag, "", "", "directory to dump to")
	pushCRIOImage.MarkFlagRequired(newRefFlag)
	pushCRIOImage.Flags().StringP(rootfsDiffPathFlag, "r", "", "crio container storage location")
	pushCRIOImage.MarkFlagRequired(rootfsDiffPathFlag)
}
