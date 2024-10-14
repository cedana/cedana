package cmd

// This file contains all the config-related commands when starting `cedana config ...`

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}
		ctx := context.WithValue(cmd.Context(), utils.CtsKey, cts)
		cmd.SetContext(ctx)
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		cts := cmd.Context().Value(utils.CtsKey).(*services.ServiceClient)
		cts.Close()
	},
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "show config that the daemon is running with",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		cts := cmd.Context().Value(utils.CtsKey).(*services.ServiceClient)

		resp, err := cts.GetConfig(ctx, &task.GetConfigRequest{})
		if err != nil {
			log.Error().Err(err).Msgf("Error getting config")
			return err
		}

		config := &types.Config{}
		err = json.Unmarshal([]byte(resp.JSON), config)
		if err != nil {
			log.Error().Err(err).Msg("failed to unmarshal json")
			return err
		}

		prettycfg, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			log.Error().Err(err).Msg("failed to parse json")
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
