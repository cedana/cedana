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
}

var RunCmd = &cobra.Command{
	Use:   "containerd <container-id>",
	Short: "Run a containerd container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid run request in context")
		}

		id := args[0]
		image, _ := cmd.Flags().GetString(containerd_flags.ImageFlag.Full)
		address, _ := cmd.Flags().GetString(containerd_flags.AddressFlag.Full)
		namespace, _ := cmd.Flags().GetString(containerd_flags.NamespaceFlag.Full)

		req.Type = "containerd"
		req.Details = &daemon.Details{Containerd: &containerd.Containerd{
			ID:        id,
			Address:   address,
			Namespace: namespace,
		}}

		if image != "" {
			req.Details.Containerd.Image = &containerd.Image{Name: image}
		}

		ctx := context.WithValue(cmd.Context(), keys.RUN_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}
