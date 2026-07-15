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

		// The JID can arrive via --jid (direct `dump slurm --jid`) or as the
		// positional argument (`dump job <JID>` invokes this command with the JID
		// as a positional arg, not the flag).
		jidStr, _ := cmd.Flags().GetString(slurm_flags.JidFlag.Full)
		if jidStr == "" {
			if pos := cmd.Flags().Args(); len(pos) > 0 {
				jidStr = pos[0]
			}
		}
		jid, err := strconv.ParseUint(jidStr, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid job id %q: %v", jidStr, err)
		}

		// PID is optional: the direct dump path passes --pid, but a daemon-managed
		// `dump job <JID>` resolves the PID server-side from the job record, so an
		// empty --pid must not be a parse error.
		var pid uint64
		if pidStr, _ := cmd.Flags().GetString(slurm_flags.PidFlag.Full); pidStr != "" {
			pid, err = strconv.ParseUint(pidStr, 10, 32)
			if err != nil {
				return fmt.Errorf("invalid pid %q: %v", pidStr, err)
			}
		}

		req.Type = "slurm"
		req.Details = &daemon.Details{Slurm: &slurmpb.Slurm{
			JobID: uint32(jid),
			PID:   uint32(pid),
		}}

		return nil
	},
}
