package cmd

import (
	"fmt"
	"strconv"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	slurmpb "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/slurm"
	"github.com/cedana/cedana/pkg/keys"
	slurm_flags "github.com/cedana/cedana/plugins/slurm/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	DumpCmd.PersistentFlags().String(slurm_flags.JidFlag.Full, "", "Slurm job id")
	DumpCmd.PersistentFlags().String(slurm_flags.PidFlag.Full, "", "PID of the job process to dump")
}

// DumpCmd is the `cedana dump slurm` subcommand. It mirrors RestoreCmd: it only
// stamps the request with the slurm type and details, letting the core dump
// command dispatch through the slurm DumpMiddleware. Unlike restore, the slurm
// dump adapters require the PID (SetPIDForDump / cgroup path resolution), so a
// --pid flag is required in addition to --jid.
var DumpCmd = &cobra.Command{
	Use:    "slurm [args...]",
	Short:  "Dump a Slurm job",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.DUMP_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid dump request in context")
		}

		jidStr, _ := cmd.Flags().GetString(slurm_flags.JidFlag.Full)
		jid, err := strconv.ParseUint(jidStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid job id %q: %v", jidStr, err)
		}

		pidStr, _ := cmd.Flags().GetString(slurm_flags.PidFlag.Full)
		pid, err := strconv.ParseUint(pidStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid pid %q: %v", pidStr, err)
		}

		req.Type = "slurm"
		req.Details = &daemon.Details{Slurm: &slurmpb.Slurm{
			JobID: uint32(jid),
			PID:   uint32(pid),
		}}

		return nil
	},
}
