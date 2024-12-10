package cmd

// Defines all reusable auto completion functions

import (
	"github.com/cedana/cedana/plugins/runc/internal/defaults"
	runc_flags "github.com/cedana/cedana/plugins/runc/pkg/flags"
	runc_client "github.com/containerd/go-runc"
	"github.com/spf13/cobra"
)

// ValidIDs returns a list of valid container IDs for shell completion
func ValidIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var root string
	root, err := cmd.Flags().GetString(runc_flags.RootFlag.Full)
	if err != nil {
		root = defaults.DEFAULT_ROOT
	}

	client := runc_client.Runc{Root: root}

	ids := []string{}
	containers, err := client.List(cmd.Context())
	if err != nil {
		return ids, cobra.ShellCompDirectiveNoFileComp
	}
	for _, container := range containers {
		ids = append(ids, container.ID)
	}

	return ids, cobra.ShellCompDirectiveNoFileComp
}
