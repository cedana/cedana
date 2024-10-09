package cmd

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/cobra"
)

var manageCmd = &cobra.Command{
	Use:   "manage",
	Short: "Start managing a process or a container",
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

var manageProcessCmd = &cobra.Command{
	Use:   "process",
	Short: "Manage a process",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

var manageRuncCmd = &cobra.Command{
	Use:   "runc",
	Short: "Manage a runc container",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// ctx := cmd.Context()

		// root, _ := cmd.Flags().GetString(rootFlag)
		// gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)

		return nil
	},
}

func init() {
	// process
	manageProcessCmd.Flags().Int(pidFlag, 0, "pid")
	manageRuncCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "runc root")
	manageCmd.AddCommand(manageProcessCmd)

	// runc
	manageRuncCmd.Flags().StringP(rootFlag, "r", "default", "runc root")
	manageRuncCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "runc root")
	manageCmd.AddCommand(manageRuncCmd)

	rootCmd.AddCommand(manageCmd)
}
