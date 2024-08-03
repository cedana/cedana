package cmd

// This file contains all the config-related commands when starting `cedana config ...`

import (
	"encoding/json"
	"fmt"

	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "show currently set configuration",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		logger := cmd.Context().Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		resp, err := cts.GetConfig(ctx, &task.GetConfigRequest{})
		if err != nil {
			logger.Error().Err(err).Msgf("Error getting config")
			return err
		}

		config := &utils.Config{}
		err = json.Unmarshal([]byte(resp.JSON), config)
		if err != nil {
			logger.Error().Err(err).Msg("failed to unmarshal json")
			return err
		}

		prettycfg, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			logger.Error().Err(err).Msg("failed to parse json")
			return err
		}
		fmt.Printf("config: %v\n", string(prettycfg))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(showCmd)
}
