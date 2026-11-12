package cmd

import (
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/podman"
	"github.com/cedana/cedana/pkg/keys"
	podman_flags "github.com/cedana/cedana/plugins/podman/pkg/flags"
	runc_flags "github.com/cedana/cedana/plugins/runc/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	RunCmd.Flags().StringP(runc_flags.IdFlag.Full, runc_flags.IdFlag.Short, "", "container name")
	RunCmd.Flags().Int32SliceP(podman_flags.GPUsFlag.Full, podman_flags.GPUsFlag.Short, []int32{}, "add GPUs to the container (e.g. 0,1,2)")
	RunCmd.Flags().StringSliceP(podman_flags.EnvFlag.Full, podman_flags.EnvFlag.Short, []string{}, "list of additional environment variables")
}

var RunCmd = &cobra.Command{
	Use:   "podman <image> [command] [args...]",
	Short: "Run a podman container",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid run request in context")
		}

		id, _ := cmd.Flags().GetString(runc_flags.IdFlag.Full)
		gpus, _ := cmd.Flags().GetInt32Slice(podman_flags.GPUsFlag.Full)
		env, _ := cmd.Flags().GetStringSlice(podman_flags.EnvFlag.Full)

		req.Type = "podman"
		req.Details = &daemon.Details{Podman: &podman.Podman{
			ID:    id,
			Image: args[0],
			Args:  args[1:],
			GPUs:  gpus,
			Env:   env,
		}}

		return nil
	},
}
