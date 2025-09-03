package cmd

import (
	"fmt"
	"io"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/spf13/cobra"
)

func init() {
	// Add flags
	checkCmd.PersistentFlags().
		BoolP(flags.FullFlag.Full, flags.FullFlag.Short, false, "perform a full check (including plugins)")
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Health check",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		full, _ := cmd.Flags().GetBool(flags.FullFlag.Full)

		logging.SetLogger(io.Discard)

		cedana, err := cedana.New(ctx, "run")
		if err != nil {
			return fmt.Errorf("Error: failed to create cedana root: %v", err)
		}

		defer cedana.Wait()
		defer cedana.Finalize()

		resp, err := cedana.HealthCheck(cmd.Context(), &daemon.HealthCheckReq{Full: full})
		if err != nil {
			return err
		}

		return printHealthCheckResults(resp.GetResults())
	},
}
