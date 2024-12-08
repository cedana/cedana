package cmd

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/keys"
	runc_flags "github.com/cedana/cedana/plugins/runc/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	RestoreCmd.Flags().StringP(runc_flags.IdFlag.Full, runc_flags.IdFlag.Short, "", "new id")
	RestoreCmd.Flags().StringP(runc_flags.RootFlag.Full, runc_flags.RootFlag.Short, "", "root")
	RestoreCmd.Flags().StringP(runc_flags.BundleFlag.Full, runc_flags.BundleFlag.Short, "", "bundle")
	RestoreCmd.MarkFlagRequired(runc_flags.BundleFlag.Full)
}

var RestoreCmd = &cobra.Command{
	Use:   "runc [optional-new-id]",
	Short: "Restore a runc container",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RESTORE_REQ_CONTEXT_KEY).(*daemon.RestoreReq)
		if !ok {
			return fmt.Errorf("invalid restore request in context")
		}

		id, _ := cmd.Flags().GetString(runc_flags.IdFlag.Full)
		root, _ := cmd.Flags().GetString(runc_flags.RootFlag.Full)
		bundle, _ := cmd.Flags().GetString(runc_flags.BundleFlag.Full)
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("Error getting working directory: %v", err)
		}

		req.Type = "runc"
		req.Details = &daemon.Details{Runc: &runc.Runc{
			ID:         id,
			Root:       root,
			Bundle:     bundle,
			WorkingDir: wd,
		}}

		ctx := context.WithValue(cmd.Context(), keys.RESTORE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}
