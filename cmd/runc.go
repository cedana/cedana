package cmd

import (
	"context"
	"fmt"

	task "buf.build/gen/go/cedana/task/protocolbuffers/go"
	"github.com/cedana/cedana/pkg/api"
	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var runcRootPath = map[string]string{
	"k8s":     api.K8S_RUNC_ROOT,
	"docker":  api.DOCKER_RUNC_ROOT,
	"default": api.DEFAULT_RUNC_ROOT,
}

func getRuncRootPath(runcRoot string) string {
	if path, ok := runcRootPath[runcRoot]; ok {
		return path
	}
	return runcRoot
}

var runcCmd = &cobra.Command{
	Use:   "runc",
	Short: "Runc container related commands",
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

var runcGetRuncIdByName = &cobra.Command{
	Use:   "get",
	Short: "Get runc id by container name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cts := cmd.Context().Value(utils.CtsKey).(*services.ServiceClient)

		root, _ := cmd.Flags().GetString(rootFlag)
		name := args[0]
		query := &task.RuncQueryArgs{
			Root:           root,
			ContainerNames: []string{name},
		}

		// FIXME YA: When no PID given, still returns something
		resp, err := cts.RuncQuery(ctx, query)
		if err != nil {
			log.Error().Err(err).Msgf("Container \"%s\" not found", name)
			return err
		}
		log.Info().Msgf("Response: %v", resp.Containers[0])

		return nil
	},
}

func init() {
	runcGetRuncIdByName.Flags().StringP(rootFlag, "r", runcRootPath["default"], "runc root directory")
	runcCmd.AddCommand(runcGetRuncIdByName)

	rootCmd.AddCommand(runcCmd)
}
