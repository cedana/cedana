package cmd

// Defines all reusable auto completion functions

import (
	"strconv"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/spf13/cobra"
)

// ValidJIDs returns a list of valid JIDs for shell completion
func ValidJIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	useVSOCK := config.Global.UseVSOCK
	var client *Client
	var err error

	if useVSOCK {
		client, err = NewVSOCKClient(config.Global.ContextID, config.Global.Port)
	} else {
		client, err = NewClient(config.Global.Host, config.Global.Port)
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
		jid := job.GetJID()
		jids = append(jids, jid)
	}

	return jids, cobra.ShellCompDirectiveNoFileComp
}

func RunningJIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	useVSOCK := config.Global.UseVSOCK
	var client *Client
	var err error

	if useVSOCK {
		client, err = NewVSOCKClient(config.Global.ContextID, config.Global.Port)
	} else {
		client, err = NewClient(config.Global.Host, config.Global.Port)
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
		if job.GetState().GetIsRunning() {
			jid := job.GetJID()
			jids = append(jids, jid)
		}
	}

	return jids, cobra.ShellCompDirectiveNoFileComp
}

// ValidPIDs returns a list of valid PIDs of jobs for shell completion
func ValidPIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	useVSOCK := config.Global.UseVSOCK
	var client *Client
	var err error

	if useVSOCK {
		client, err = NewVSOCKClient(config.Global.ContextID, config.Global.Port)
	} else {
		client, err = NewClient(config.Global.Host, config.Global.Port)
	}
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	pids := []string{}
	resp, err := client.List(cmd.Context(), &daemon.ListReq{})
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	for _, job := range resp.Jobs {
		pidInt := int(job.GetState().GetPID())
		if pidInt == 0 {
			continue
		}
		pid := strconv.Itoa(pidInt)
		pids = append(pids, pid)
	}

	return pids, cobra.ShellCompDirectiveNoFileComp
}

// ValidPlugins returns a list of valid plugin names for shell completion
func ValidPlugins(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	manager := plugins.NewLocalManager()

	list, err := manager.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	names := []string{}
	for _, plugin := range list {
		names = append(names, plugin.Name)
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}
