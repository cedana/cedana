package cmd

import (
	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

var dumpCRIORootfs = &cobra.Command{
	Use:   "crioRootfs",
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
		dest, err := cmd.Flags().GetString(destFlag)
		if err != nil {
			logger.Error().Msgf("Error getting destination path: %v", err)
		}
		containerStorage, err := cmd.Flags().GetString(containerStorageFlag)
		if err != nil {
			logger.Error().Msgf("Error getting container storage path: %v", err)
		}

		dumpArgs := task.CRIORootfsDumpArgs{
			ContainerID:      id,
			Dest:             dest,
			ContainerStorage: containerStorage,
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
		logger.Info().Msgf("Response: %v", resp)

		return nil
	},
}

var pushCRIOImage = &cobra.Command{
	Use:   "crio-push",
	Short: "Manually push a crio image",
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

		originalImageRef, err := cmd.Flags().GetString(refFlag)
		if err != nil {
			logger.Error().Msgf("Error getting container id: %v", err)
		}
		newImageRef, err := cmd.Flags().GetString(newRefFlag)
		if err != nil {
			logger.Error().Msgf("Error getting destination path: %v", err)
		}
		rootfsDiffPath, err := cmd.Flags().GetString(rootfsDiffPathFlag)
		if err != nil {
			logger.Error().Msgf("Error getting container storage path: %v", err)
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
				logger.Error().Msgf("Checkpoint task failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
			return err
		}
		logger.Info().Msgf("Response: %v", resp)

		return nil
	},
}
