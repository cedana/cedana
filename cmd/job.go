package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

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
	jobCmd.AddCommand(killJobCmd)
	jobCmd.AddCommand(deleteJobCmd)
	jobCmd.AddCommand(attachJobCmd)

	// Add subcommand flags
	deleteJobCmd.Flags().BoolP(types.AllFlag.Full, types.AllFlag.Short, false, "delete all jobs")
	killJobCmd.Flags().BoolP(types.AllFlag.Full, types.AllFlag.Short, false, "kill all jobs")

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

////////////////////
/// Subcommands  ///
////////////////////

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

		writer.AppendHeader(table.Row{"Job", "Type", "PID", "State", "Last Checkpoint", "Std I/O", "GPU"})

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
				job.GetProcess().GetPID(),
				state,
				job.GetCheckpointPath(),
				job.GetLog(),
				boolStr(job.GetGPUEnabled()),
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
		var jid string
		req := &daemon.KillReq{}

		if len(args) == 1 {
			jid = args[0]
			req.JIDs = []string{jid}
		} else {
			// Check if the all flag is set
			all, _ := cmd.Flags().GetBool(types.AllFlag.Full)
			if !all {
				return fmt.Errorf("Please provide a job ID or use the --all flag")
			}
		}

		client := utils.GetContextValSafe(cmd.Context(), types.CLIENT_CONTEXT_KEY, &Client{})
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
		var jid string
		req := &daemon.DeleteReq{}

		if len(args) == 1 {
			jid = args[0]
			req.JIDs = []string{jid}
		} else {
			// Check if the all flag is set
			all, _ := cmd.Flags().GetBool(types.AllFlag.Full)
			if !all {
				return fmt.Errorf("Please provide a job ID or use the --all flag")
			}
		}

		client := utils.GetContextValSafe(cmd.Context(), types.CLIENT_CONTEXT_KEY, &Client{})
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
		client := utils.GetContextValSafe(cmd.Context(), types.CLIENT_CONTEXT_KEY, &Client{})

		jid := args[0]

		stream, err := client.Attach(cmd.Context(), &daemon.AttachReq{JID: jid})
		if err != nil {
			return err
		}
		stdIn, stdOut, stdErr, _ := utils.NewStreamIOMaster(stream)

		go io.Copy(stdIn, os.Stdin)           // since stdin never closes
		<-utils.CopyNotify(os.Stdout, stdOut) // wait to capture all out
		<-utils.CopyNotify(os.Stderr, stdErr) // wait to capture all err

		// os.Exit(<-exitCode)

		return nil
	},
}

////////////////////
///// Aliases //////
////////////////////

var psCmd = &cobra.Command{
	Use:   utils.AliasCommandUse(listJobCmd, "ps"),
	Short: listJobCmd.Short,
	Long:  listJobCmd.Long,
	Args:  listJobCmd.Args,
	RunE:  utils.AliasCommandRunE(listJobCmd),
}

var attachCmd = &cobra.Command{
	Use:   utils.AliasCommandUse(attachJobCmd, "attach"),
	Short: attachJobCmd.Short,
	Long:  attachJobCmd.Long,
	Args:  attachJobCmd.Args,
	RunE:  utils.AliasCommandRunE(attachJobCmd),
}
