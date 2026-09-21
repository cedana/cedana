package cmd

import (
	"fmt"
	"strconv"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	slurmpb "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/slurm"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	slurm_flags "github.com/cedana/cedana/plugins/slurm/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	RestoreCmd.PersistentFlags().StringP(flags.JidFlag.Full, flags.JidFlag.Short, "", "Slurm job id")
}

var RestoreCmd = &cobra.Command{
	Use:    "slurm [args...]",
	Short:  "Restore a Slurm job",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RESTORE_REQ_CONTEXT_KEY).(*daemon.RestoreReq)
		if !ok {
			return fmt.Errorf("invalid restore request in context")
		}

		jidStr, _ := cmd.Flags().GetString(slurm_flags.JidFlag.Full)
		jid, err := strconv.ParseUint(jidStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid job id: %v", err)
		}

		req.Type = "slurm"
		req.Details = &daemon.Details{Slurm: &slurmpb.Slurm{
			JobID: uint32(jid),
		}}

		return nil
	},
}
