package cmd

// Defines all reusable auto completion functions

import (
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/spf13/cobra"
)

// ValidJIDs returns a list of valid JIDs for shell completion
func ValidJIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	useVSOCK, _ := cmd.Flags().GetBool(flags.UseVSOCKFlag.Full)
	var client *Client
	var err error

	if useVSOCK {
		client, err = NewVSOCKClient(config.Get(config.VSOCK_CONTEXT_ID), config.Get(config.PORT))
	} else {
		client, err = NewClient(config.Get(config.HOST), config.Get(config.PORT))
	}
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	jids := []string{}
	resp, err := client.List(cmd.Context(), &daemon.ListReq{})
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	for _, job := range resp.Jobs {
		jids = append(jids, job.GetJID())
	}

	return jids, cobra.ShellCompDirectiveNoFileComp
}
