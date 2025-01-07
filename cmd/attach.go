package cmd

import (
	"fmt"
	"strconv"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/spf13/cobra"
)

// Parent attach command
var attachCmd = &cobra.Command{
	Use:               "attach <PID>",
	Short:             "Attach stdin/out/err to a process/container",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: ValidPIDs,
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		client, err := NewClient(config.Global.Address, config.Global.Protocol)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}

		pid, err := strconv.ParseUint(args[0], 10, 32)
		if err != nil {
			return fmt.Errorf("invalid pid: %v", err)
		}

		return client.Attach(cmd.Context(), &daemon.AttachReq{PID: uint32(pid)})
	},
}
