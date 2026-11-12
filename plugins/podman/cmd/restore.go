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
	RestoreCmd.Flags().StringP(runc_flags.IdFlag.Full, runc_flags.IdFlag.Short, "", "new container name")
	RestoreCmd.Flags().StringP(podman_flags.ImageFlag.Full, podman_flags.ImageFlag.Short, "", "image to use (default: same as dumped container)")
	RestoreCmd.Flags().Int32SliceP(podman_flags.GPUsFlag.Full, podman_flags.GPUsFlag.Short, []int32{}, "add GPUs to the container (e.g. 0,1,2)")
	RestoreCmd.Flags().StringSliceP(podman_flags.EnvFlag.Full, podman_flags.EnvFlag.Short, []string{}, "list of additional environment variables")
}

var RestoreCmd = &cobra.Command{
	Use:   "podman",
	Short: "Restore a podman container",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RESTORE_REQ_CONTEXT_KEY).(*daemon.RestoreReq)
		if !ok {
			return fmt.Errorf("invalid restore request in context")
		}

		id, _ := cmd.Flags().GetString(runc_flags.IdFlag.Full)
		image, _ := cmd.Flags().GetString(podman_flags.ImageFlag.Full)
		gpus, _ := cmd.Flags().GetInt32Slice(podman_flags.GPUsFlag.Full)
		env, _ := cmd.Flags().GetStringSlice(podman_flags.EnvFlag.Full)

		req.Type = "podman"
		req.Details = &daemon.Details{Podman: &podman.Podman{
			ID:    id,
			Image: image,
			GPUs:  gpus,
			Env:   env,
		}}

		return nil
	},
}
