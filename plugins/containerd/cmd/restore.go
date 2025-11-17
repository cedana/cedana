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
	RestoreCmd.Flags().StringP(runc_flags.IdFlag.Full, runc_flags.IdFlag.Short, "", "new id")
	RestoreCmd.Flags().StringP(containerd_flags.ImageFlag.Full, containerd_flags.ImageFlag.Short, "", "image to use")
	RestoreCmd.Flags().StringP(containerd_flags.AddressFlag.Full, containerd_flags.AddressFlag.Short, "", "containerd socket address")
	RestoreCmd.Flags().StringP(containerd_flags.NamespaceFlag.Full, containerd_flags.NamespaceFlag.Short, "", "containerd namespace")
	RestoreCmd.Flags().Int32SliceP(containerd_flags.GPUsFlag.Full, containerd_flags.GPUsFlag.Short, []int32{}, "add GPUs to the container (e.g. 0,1,2)")
	RestoreCmd.Flags().BoolP(runc_flags.NoPivotFlag.Full, runc_flags.NoPivotFlag.Short, false, "disable use of pivot-root")
	RestoreCmd.Flags().StringSliceP(containerd_flags.EnvFlag.Full, containerd_flags.EnvFlag.Short, []string{}, "list of additional environment variables")
}

var RestoreCmd = &cobra.Command{
	Use:   "containerd [container-id] [args...]",
	Short: "Restore a containerd container",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RESTORE_REQ_CONTEXT_KEY).(*daemon.RestoreReq)
		if !ok {
			return fmt.Errorf("invalid restore request in context")
		}

		id, _ := cmd.Flags().GetString(runc_flags.IdFlag.Full)
		image, _ := cmd.Flags().GetString(containerd_flags.ImageFlag.Full)
		address, _ := cmd.Flags().GetString(containerd_flags.AddressFlag.Full)
		namespace, _ := cmd.Flags().GetString(containerd_flags.NamespaceFlag.Full)
		gpus, _ := cmd.Flags().GetInt32Slice(containerd_flags.GPUsFlag.Full)
		noPivot, _ := cmd.Flags().GetBool(runc_flags.NoPivotFlag.Full)
		env, _ := cmd.Flags().GetStringSlice(containerd_flags.EnvFlag.Full)

		req.Type = "containerd"
		req.Details = &daemon.Details{Containerd: &containerd.Containerd{
			ID:        id,
			Address:   address,
			Namespace: namespace,
			GPUs:      gpus,
			NoPivot:   noPivot,
			Env:       env,
		}}

		if image != "" {
			req.Details.Containerd.Image = &containerd.Image{Name: image}
		}

		return nil
	},
}
