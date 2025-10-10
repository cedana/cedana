package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/pkg/keys"
	containerd_flags "github.com/cedana/cedana/plugins/containerd/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	RunCmd.Flags().StringP(containerd_flags.ImageFlag.Full, containerd_flags.ImageFlag.Short, "", "image to use")
	RunCmd.Flags().StringP(containerd_flags.AddressFlag.Full, containerd_flags.AddressFlag.Short, "", "containerd socket address")
	RunCmd.Flags().StringP(containerd_flags.NamespaceFlag.Full, containerd_flags.NamespaceFlag.Short, "", "containerd namespace")
	RunCmd.Flags().Int32SliceP(containerd_flags.GPUsFlag.Full, containerd_flags.GPUsFlag.Short, []int32{}, "Add GPUs to the container (e.g. 0,1,2)")
}

var RunCmd = &cobra.Command{
	Use:   "containerd [container-id] [args...]",
	Short: "Run a containerd container",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid run request in context")
		}

		var id string
		var procArgs []string

		if len(args) > 0 {
			id = args[0]
			procArgs = args[1:]
		}

		image, _ := cmd.Flags().GetString(containerd_flags.ImageFlag.Full)
		address, _ := cmd.Flags().GetString(containerd_flags.AddressFlag.Full)
		namespace, _ := cmd.Flags().GetString(containerd_flags.NamespaceFlag.Full)
		gpus, _ := cmd.Flags().GetInt32Slice(containerd_flags.GPUsFlag.Full)

		req.Type = "containerd"
		req.Details = &daemon.Details{Containerd: &containerd.Containerd{
			ID:        id,
			Address:   address,
			Namespace: namespace,
			GPUs:      gpus,
			Args:      procArgs,
		}}

		if image != "" {
			req.Details.Containerd.Image = &containerd.Image{Name: image}
		}

		ctx := context.WithValue(cmd.Context(), keys.RUN_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}
