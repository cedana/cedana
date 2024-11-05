package cmd

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	jobCmd.AddCommand(listJobCmd)

	// Sync flags with aliases
	psCmd.Flags().AddFlagSet(listJobCmd.PersistentFlags())
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
			return nil
		}

		writer := table.NewWriter()
		writer.SetOutputMirror(cmd.OutOrStdout())
		writer.SetStyle(table.StyleLight)
		writer.Style().Options.SeparateRows = false

		writer.AppendHeader(table.Row{"Job", "Type", "State", "Last Checkpoint", "GPU"})

		boolStr := func(b bool) string {
			if b {
				return text.Colors{text.FgGreen}.Sprint("yes")
			}
			return text.Colors{text.FgRed}.Sprint("no")
		}

		for _, job := range jobs {
			state := text.Colors{text.FgGreen}.Sprint("running")
			if !job.GetProcess().GetInfo().GetIsRunning() {
				state = text.Colors{text.FgRed}.Sprint("exited")
			}
			row := table.Row{
				job.GetJID(),
				job.GetType(),
				state,
				job.GetCheckpointPath(),
				boolStr(job.GetGPUEnabled()),
			}
			writer.AppendRow(row)
		}

		writer.Render()

		return nil
	},
}

////////////////////
///// Aliases //////
////////////////////

var psCmd = &cobra.Command{
	Use:        utils.AliasCommandUse(listJobCmd, "ps"),
	Short:      listJobCmd.Short,
	Long:       listJobCmd.Long,
	Deprecated: "Use `job list` instead",
	RunE:       utils.AliasCommandRunE(listJobCmd),
}
