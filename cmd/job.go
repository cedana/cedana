package cmd

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	jobCmd.AddCommand(listJobCmd)
}

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage jobs",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		port := viper.GetUint32("options.port")
		host := viper.GetString("options.host")

		client, err := NewClient(host, port)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}

		ctx := context.WithValue(cmd.Context(), types.CLIENT_CONTEXT_KEY, client)
		cmd.SetContext(ctx)

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		client := utils.GetContextValSafe(cmd.Context(), types.CLIENT_CONTEXT_KEY, &Client{})
		client.Close()
		return nil
	},
}

var listJobCmd = &cobra.Command{
	Use:   "list",
	Short: "List all managed processes/containers (jobs)",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := utils.GetContextValSafe(cmd.Context(), types.CLIENT_CONTEXT_KEY, &Client{})

		resp, err := client.List(cmd.Context(), &daemon.ListReq{})
		if err != nil {
			return err
		}
		jobs := resp.Jobs

		if len(jobs) == 0 {
			fmt.Println("No jobs found")
		}

		writer := table.NewWriter()
		writer.AppendHeader(table.Row{"JID", "Type", "State", "Checkpoint", "GPU Enabled"})

		for _, job := range jobs {
			writer.AppendRow([]interface{}{job.JID, job.Type, job.Process.Info.GetStatus, job.CheckpointPath, job.GPUEnabled})
		}

		writer.Render()

		return nil
	},
}

////////////////////
///// Aliases //////
////////////////////

var psCmd = &cobra.Command{
	Use:        "ps",
	Short:      "List all managed processes/containers (jobs)",
	Deprecated: "Use `job list` instead",
	Long:       "Alias for `job list`",
	RunE:       utils.AliasRunE(listJobCmd),
}
