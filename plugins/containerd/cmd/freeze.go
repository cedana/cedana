package cmd

import (
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/pkg/keys"
	containerd_flags "github.com/cedana/cedana/plugins/containerd/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	FreezeCmd.Flags().StringP(containerd_flags.AddressFlag.Full, containerd_flags.AddressFlag.Short, "", "containerd socket address")
	FreezeCmd.Flags().StringP(containerd_flags.NamespaceFlag.Full, containerd_flags.NamespaceFlag.Short, "", "containerd namespace")
}

var FreezeCmd = &cobra.Command{
	Use:   "containerd <container-id>",
	Short: "Freeze a containerd container",
	Long:  "Freeze a containerd container",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.FREEZE_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid freeze request in context")
		}

		var id string
		if len(args) > 0 {
			id = args[0]
		}

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

		return nil
	},
}
