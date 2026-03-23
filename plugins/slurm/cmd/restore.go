package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	slurmpb "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/slurm"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

func init() {
	RestoreCmd.PersistentFlags().
		StringP(flags.PathFlag.Full, flags.PathFlag.Short, "", "path of dump")
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

		jobID, err := cmd.Flags().GetUint32(flags.JidFlag.Full)
		if err != nil {
			return fmt.Errorf("invalid job id: %v", err)
		}

		path, err := cmd.Flags().GetString(flags.PathFlag.Full)
		if err != nil {
			return fmt.Errorf("invalid path: %v", err)
		}

		req.Type = "slurm"
		req.Details = &daemon.Details{Slurm: &slurmpb.Slurm{
			ID:    fmt.Sprintf("%d", jobID),
			JobID: uint32(jobID),
		}}

		req.Path = path
		req.Criu = &criu.CriuOpts{
			Unprivileged:   proto.Bool(true),
			ShellJob:       proto.Bool(true),
			TcpEstablished: proto.Bool(true),
			FileLocks:      proto.Bool(true),
			LinkRemap:      proto.Bool(true),
		}

		ctx := context.WithValue(cmd.Context(), keys.RESTORE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}
