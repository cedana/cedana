package cmd

import (
	"fmt"
	"strconv"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/config"
	"github.com/spf13/cobra"
)

// Parent attach command
var attachCmd = &cobra.Command{
	Use:   "attach <PID>",
	Short: "Attach stdin/out/err to a process/container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := NewClient(config.Get(config.HOST), config.Get(config.PORT))
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}

		pid, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("PID must be an integer")
		}

		return client.Attach(cmd.Context(), &daemon.AttachReq{PID: uint32(pid)})
	},
}
