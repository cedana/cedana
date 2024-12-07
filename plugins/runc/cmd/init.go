package cmd

import (
	"github.com/opencontainers/runc/libcontainer"
	"github.com/spf13/cobra"
)

// Implement the entrypoint for runc init. This command must NOT be called directly
// by the user. It is called automatically when creating a new runc container.

var InitCmd = &cobra.Command{
	Use:    "init",
	Short:  "Runc init entrypoint [DO NOT USE]",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		libcontainer.Init()
	},
}
