package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func init() {
	jobCmd.AddCommand(listJobCmd)
	jobCmd.AddCommand(killJobCmd)
	jobCmd.AddCommand(deleteJobCmd)
	jobCmd.AddCommand(attachJobCmd)

	// Add subcommand flags
	deleteJobCmd.Flags().BoolP(flags.AllFlag.Full, flags.AllFlag.Short, false, "delete all jobs")
	killJobCmd.Flags().BoolP(flags.AllFlag.Full, flags.AllFlag.Short, false, "kill all jobs")

	// Add aliases
	rootCmd.AddCommand(utils.AliasOf(listJobCmd, "ps"))
	rootCmd.AddCommand(utils.AliasOf(deleteJobCmd))
	rootCmd.AddCommand(utils.AliasOf(killJobCmd))
}

// Parent job command
var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage jobs",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		client, err := NewClient(config.Get(config.HOST), config.Get(config.PORT))
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}

		ctx := context.WithValue(cmd.Context(), keys.CLIENT_CONTEXT_KEY, client)
		cmd.SetContext(ctx)

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		client := utils.GetContextValSafe(cmd.Context(), keys.CLIENT_CONTEXT_KEY, &Client{})
		client.Close()
		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

var listJobCmd = &cobra.Command{
	Use:   "list",
	Short: "List all managed processes/containers (jobs)",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := utils.GetContextValSafe(cmd.Context(), keys.CLIENT_CONTEXT_KEY, &Client{})

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
		writer.SetStyle(style.TableStyle)
		writer.Style().Options.SeparateRows = false

		writer.AppendHeader(table.Row{
			"Job",
			"Type",
			"PID",
			"State",
			"Std I/O",
			"Last Checkpoint",
			"GPU",
		})

		for _, job := range jobs {
			row := table.Row{
				job.GetJID(),
				job.GetType(),
				job.GetProcess().GetPID(),
				style.BoolStr(job.GetProcess().GetInfo().GetIsRunning(), "running", "stopped"),
				job.GetLog(),
				job.GetCheckpointPath(),
				style.BoolStr(job.GetGPUEnabled()),
			}
			writer.AppendRow(row)
		}

		writer.Render()

		return nil
	},
}

var killJobCmd = &cobra.Command{
	Use:   "kill <JID>",
	Short: "Kill a managed process/container (job)",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := utils.GetContextValSafe(cmd.Context(), keys.CLIENT_CONTEXT_KEY, &Client{})

		var jid string
		req := &daemon.KillReq{}

		if len(args) == 1 {
			jid = args[0]
			req.JIDs = []string{jid}
		} else {
			// Check if the all flag is set
			all, _ := cmd.Flags().GetBool(flags.AllFlag.Full)
			if !all {
				return fmt.Errorf("Please provide a job ID or use the --all flag")
			}
		}

		_, err := client.Kill(cmd.Context(), req)
		if err != nil {
			return err
		}

		if jid != "" {
			fmt.Printf("Killed job %s\n", jid)
		} else {
			fmt.Println("Killed jobs")
		}

		return nil
	},
}

var deleteJobCmd = &cobra.Command{
	Use:   "delete <JID>",
	Short: "Delete a managed process/container (job)",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := utils.GetContextValSafe(cmd.Context(), keys.CLIENT_CONTEXT_KEY, &Client{})

		var jid string
		req := &daemon.DeleteReq{}

		if len(args) == 1 {
			jid = args[0]
			req.JIDs = []string{jid}
		} else {
			// Check if the all flag is set
			all, _ := cmd.Flags().GetBool(flags.AllFlag.Full)
			if !all {
				return fmt.Errorf("Please provide a job ID or use the --all flag")
			}
		}

		_, err := client.Delete(cmd.Context(), req)
		if err != nil {
			return err
		}

		if jid != "" {
			fmt.Printf("Deleted job %s\n", jid)
		} else {
			fmt.Println("Deleted jobs")
		}

		return nil
	},
}

var attachJobCmd = &cobra.Command{
	Use:   "attach <JID>",
	Short: "Attach stdin/out/err to a managed process/container (job)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := utils.GetContextValSafe(cmd.Context(), keys.CLIENT_CONTEXT_KEY, &Client{})

		jid := args[0]

		list, err := client.List(cmd.Context(), &daemon.ListReq{JIDs: []string{jid}})
		if err != nil {
			return err
		}
		if len(list.Jobs) == 0 {
			return fmt.Errorf("Job %s not found", jid)
		}

		pid := list.Jobs[0].GetDetails().GetPID()

		return client.Attach(cmd.Context(), &daemon.AttachReq{PID: pid})
	},
}
