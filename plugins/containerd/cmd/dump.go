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
	DumpCmd.Flags().StringP(containerd_flags.ImageFlag.Full, containerd_flags.ImageFlag.Short, "", "image ref (rootfs). leave empty to skip rootfs")
	DumpCmd.Flags().StringP(containerd_flags.AddressFlag.Full, containerd_flags.AddressFlag.Short, "", "containerd socket address")
	DumpCmd.Flags().StringP(containerd_flags.NamespaceFlag.Full, containerd_flags.NamespaceFlag.Short, "", "containerd namespace")
	DumpCmd.Flags().BoolP(containerd_flags.RootfsFlag.Full, containerd_flags.RootfsFlag.Short, false, "dump with rootfs")
	DumpCmd.Flags().BoolP(containerd_flags.RootfsOnlyFlag.Full, containerd_flags.RootfsOnlyFlag.Short, false, "dump only the rootfs")
}

var DumpCmd = &cobra.Command{
	Use:   "containerd <container-id>",
	Short: "Dump a containerd container (w/ rootfs)",
	Long:  "Dump a containerd container (w/ rootfs). If an image ref is provided, rootfs will also be dumped",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.DUMP_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid dump request in context")
		}

		var id string
		if len(args) > 0 {
			id = args[0]
		}

		image, _ := cmd.Flags().GetString(containerd_flags.ImageFlag.Full)
		address, _ := cmd.Flags().GetString(containerd_flags.AddressFlag.Full)
		namespace, _ := cmd.Flags().GetString(containerd_flags.NamespaceFlag.Full)
		rootfs, _ := cmd.Flags().GetBool(containerd_flags.RootfsFlag.Full)
		rootfsOnly, _ := cmd.Flags().GetBool(containerd_flags.RootfsOnlyFlag.Full)

		req.Type = "containerd"
		req.Details = &daemon.Details{Containerd: &containerd.Containerd{
			ID:         id,
			Address:    address,
			Namespace:  namespace,
			Rootfs:     rootfs,
			RootfsOnly: rootfsOnly,
		}}

		if image != "" {
			req.Details.Containerd.Image = &containerd.Image{Name: image}
		}

		return nil
	},
}
