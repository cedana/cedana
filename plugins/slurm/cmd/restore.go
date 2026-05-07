package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	slurmpb "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/slurm"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	slurm_flags "github.com/cedana/cedana/plugins/slurm/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	RestoreCmd.PersistentFlags().
		StringP(flags.JidFlag.Full, flags.JidFlag.Short, "", "Slurm job id")
}

var RestoreCmd = &cobra.Command{
	Use:    "slurm --jid [jobID] --path [path]",
	Short:  "Restore a Slurm job",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// All we need to do is modify the request type
		req, ok := cmd.Context().Value(keys.RESTORE_REQ_CONTEXT_KEY).(*daemon.RestoreReq)
		if !ok {
			return fmt.Errorf("invalid restore request in context")
		}

		jobID, err := cmd.Flags().GetString(slurm_flags.JidFlag.Full)
		if err != nil {
			return fmt.Errorf("invalid job id: %v", err)
		}

		jid, err := strconv.ParseUint(jobID, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid job id: %v", err)
		}

		path, err := cmd.Flags().GetString(flags.PathFlag.Full)
		if err != nil {
			return fmt.Errorf("invalid path: %v", err)
		}

		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to get hostname: %v", err)
		}

		req.Type = "slurm"
		req.Details = &daemon.Details{Slurm: &slurmpb.Slurm{
			ID:       fmt.Sprintf("%s", jobID),
			JobID:    uint32(jid),
			Hostname: hostname,
		}}

		req.Path = path

		ctx := context.WithValue(cmd.Context(), keys.RESTORE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}
