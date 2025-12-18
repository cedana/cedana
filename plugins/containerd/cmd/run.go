package cmd

import (
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/pkg/keys"
	containerd_flags "github.com/cedana/cedana/plugins/containerd/pkg/flags"
	runc_flags "github.com/cedana/cedana/plugins/runc/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	RunCmd.Flags().StringP(runc_flags.IdFlag.Full, runc_flags.IdFlag.Short, "", "containerd id")
	RunCmd.Flags().StringP(containerd_flags.AddressFlag.Full, containerd_flags.AddressFlag.Short, "", "containerd socket address")
	RunCmd.Flags().StringP(containerd_flags.NamespaceFlag.Full, containerd_flags.NamespaceFlag.Short, "", "containerd namespace")
	RunCmd.Flags().Int32SliceP(containerd_flags.GPUsFlag.Full, containerd_flags.GPUsFlag.Short, []int32{}, "add GPUs to the container (e.g. 0,1,2)")
	RunCmd.Flags().BoolP(runc_flags.NoPivotFlag.Full, runc_flags.NoPivotFlag.Short, false, "disable use of pivot-root")
	RunCmd.Flags().StringSliceP(containerd_flags.EnvFlag.Full, containerd_flags.EnvFlag.Short, []string{}, "list of additional environment variables")
	RunCmd.Flags().StringP(containerd_flags.SnapshotterFlag.Full, containerd_flags.SnapshotterFlag.Short, "", "containerd snapshotter to use")
}

var RunCmd = &cobra.Command{
	Use:   "containerd <image|rootfs> [command] [args...]",
	Short: "Run a containerd container",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid run request in context")
		}

		image := args[0]
		procArgs := args[1:]

		id, _ := cmd.Flags().GetString(runc_flags.IdFlag.Full)
		address, _ := cmd.Flags().GetString(containerd_flags.AddressFlag.Full)
		namespace, _ := cmd.Flags().GetString(containerd_flags.NamespaceFlag.Full)
		gpus, _ := cmd.Flags().GetInt32Slice(containerd_flags.GPUsFlag.Full)
		noPivot, _ := cmd.Flags().GetBool(runc_flags.NoPivotFlag.Full)
		env, _ := cmd.Flags().GetStringSlice(containerd_flags.EnvFlag.Full)
		snapshotter, _ := cmd.Flags().GetString(containerd_flags.SnapshotterFlag.Full)

		req.Type = "containerd"
		req.Details = &daemon.Details{Containerd: &containerd.Containerd{
			ID:          id,
			Address:     address,
			Namespace:   namespace,
			GPUs:        gpus,
			NoPivot:     noPivot,
			Args:        procArgs,
			Env:         env,
			Snapshotter: snapshotter,
		}}

		if image != "" {
			req.Details.Containerd.Image = &containerd.Image{Name: image}
		}

		return nil
	},
}
