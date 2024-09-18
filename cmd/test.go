package cmd

// This file contains all the ps-related commands when starting `cedana ps ...`

import (
	"fmt"
	"os"
	"strconv"

	"github.com/cedana/cedana/pkg/jobservice"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "test command",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log := log.Ctx(ctx)
		containerConfig, err := jobservice.FindContainerdConfig(ctx)
		if err != nil {
			return err
		}
		log.Debug().Msgf("sock: %s", containerConfig.HLSockAddr)
		log.Debug().Msgf("root: %s", containerConfig.LLRoot)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(testCmd)
}
