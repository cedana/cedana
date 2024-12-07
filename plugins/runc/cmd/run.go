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
	RunCmd.Flags().StringP(runc_flags.RootFlag.Full, runc_flags.RootFlag.Short, "", "runc root")
	RunCmd.Flags().StringP(runc_flags.BundleFlag.Full, runc_flags.BundleFlag.Short, "", "runc bundle")
	RunCmd.Flags().BoolP(runc_flags.NoPivotFlag.Full, runc_flags.NoPivotFlag.Short, false, "do not use pivot root to jail process inside rootfs.")
	RunCmd.Flags().BoolP(runc_flags.NoNewKeyringFlag.Full, runc_flags.NoNewKeyringFlag.Short, false, "do not create a new session keyring.")
	RunCmd.MarkFlagRequired(runc_flags.BundleFlag.Full)
}

var RunCmd = &cobra.Command{
	Use:   "runc [container-id]",
	Short: "run a runc container",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid run request in context")
		}

		var id string
		if len(args) >= 1 {
			id = args[0]
		}

		root, _ := cmd.Flags().GetString(runc_flags.RootFlag.Full)
		bundle, _ := cmd.Flags().GetString(runc_flags.BundleFlag.Full)
		noPivot, _ := cmd.Flags().GetBool(runc_flags.NoPivotFlag.Full)
		noNewKeyring, _ := cmd.Flags().GetBool(runc_flags.NoNewKeyringFlag.Full)
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("Error getting working directory: %v", err)
		}

		req.Type = "runc"
		req.Details = &daemon.Details{Runc: &runc.Runc{
			Root:         root,
			Bundle:       bundle,
			ID:           id,
			NoPivot:      noPivot,
			NoNewKeyring: noNewKeyring,
			WorkingDir:   wd,
		}}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}
