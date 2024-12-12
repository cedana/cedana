package cmd

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/features"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	jobCmd.AddCommand(listJobCmd)
	jobCmd.AddCommand(killJobCmd)
	jobCmd.AddCommand(deleteJobCmd)
	jobCmd.AddCommand(attachJobCmd)
	jobCmd.AddCommand(inspectJobCmd)
	jobCmd.AddCommand(jobCheckpointCmd)

	jobCheckpointCmd.AddCommand(listJobCheckpointCmd)
	jobCheckpointCmd.AddCommand(inspectJobCheckpointCmd)

	// Add subcommand flags
	deleteJobCmd.Flags().BoolP(flags.AllFlag.Full, flags.AllFlag.Short, false, "delete all jobs")
	killJobCmd.Flags().BoolP(flags.AllFlag.Full, flags.AllFlag.Short, false, "kill all jobs")
	inspectJobCheckpointCmd.Flags().StringP(flags.TypeFlag.Full, flags.TypeFlag.Short, "", "specify image file {ps|fd|mem|rss|sk|gpu}")

	// Add aliases
	jobCmd.AddCommand(utils.AliasOf(listJobCheckpointCmd, "checkpoints"))
	rootCmd.AddCommand(utils.AliasOf(listJobCmd, "ps"))
	rootCmd.AddCommand(utils.AliasOf(listJobCmd, "jobs"))
	rootCmd.AddCommand(utils.AliasOf(deleteJobCmd))
	rootCmd.AddCommand(utils.AliasOf(killJobCmd))
	rootCmd.AddCommand(utils.AliasOf(inspectJobCmd))
	rootCmd.AddCommand(utils.AliasOf(jobCheckpointCmd))
	rootCmd.AddCommand(utils.AliasOf(listJobCheckpointCmd, "checkpoints"))
}

// Parent job command
var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage jobs",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
		useVSOCK, _ := cmd.Flags().GetBool(flags.UseVSOCKFlag.Full)
		var client *Client

		if useVSOCK {
			client, err = NewVSOCKClient(config.Get(config.VSOCK_CONTEXT_ID), config.Get(config.PORT))
		} else {
			client, err = NewClient(config.Get(config.HOST), config.Get(config.PORT))
		}
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}

		ctx := context.WithValue(cmd.Context(), keys.CLIENT_CONTEXT_KEY, client)
		cmd.SetContext(ctx)

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}
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
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		resp, err := client.List(cmd.Context(), &daemon.ListReq{})
		if err != nil {
			return err
		}
		jobs := resp.Jobs

		if len(jobs) == 0 {
			fmt.Println("No jobs found")
			return nil
		}

		style.TableWriter.AppendHeader(table.Row{
			"Job",
			"Type",
			"PID",
			"Status",
			"GPU",
			"Checkpoint",
			"Size",
			"Std I/O",
		})

		style.TableWriter.SortBy([]table.SortBy{
			{Name: "Status", Mode: table.Dsc},
			{Name: "Checkpoint"},
		})

		statusStr := func(status string) string {
			switch status {
			case "running":
				return style.PositiveColor.Sprintf(status)
			case "sleep":
				return style.InfoColor.Sprintf(status)
			case "zombie":
				return style.WarningColor.Sprintf(status)
			case "halted":
				return style.DisbledColor.Sprintf(status)
			}
			return style.DisbledColor.Sprintf(status)
		}

		// Color type based on the plugin theme
		typeStr := func(t string) string {
			colorToUse := text.Colors{}
			features.CmdTheme.IfAvailable(func(name string, theme text.Colors) error {
				colorToUse = theme
				return nil
			}, t)
			return colorToUse.Sprintf(t)
		}

		latestCheckpoint := func(checkpoints []*daemon.Checkpoint) (string, string) {
			if len(checkpoints) == 0 {
				return "", ""
			}
			// sort checkpoints by time
			sort.Slice(checkpoints, func(i, j int) bool {
				return checkpoints[i].GetTime() > checkpoints[j].GetTime()
			})
			checkpoint := checkpoints[0]
			timestamp := time.UnixMilli(checkpoint.GetTime())
			return fmt.Sprintf("%s ago", time.Since(timestamp).Truncate(time.Second)), utils.SizeStr(checkpoint.GetSize())
		}

		var timeList []string
		var sizeList []string
		for _, job := range jobs {
			if len(job.GetCheckpoints()) == 0 {
				timeList = append(timeList, "")
				sizeList = append(sizeList, "")
			} else {
				latestTime, latestSize := latestCheckpoint(job.GetCheckpoints())
				timeList = append(timeList, latestTime)
				sizeList = append(sizeList, latestSize)
			}
		}

		for i, job := range jobs {
			row := table.Row{
				job.GetJID(),
				typeStr(job.GetType()),
				job.GetProcess().GetPID(),
				statusStr(job.GetProcess().GetInfo().GetStatus()),
				style.BoolStr(job.GetGPUEnabled()),
				timeList[i],
				sizeList[i],
				job.GetLog(),
			}
			style.TableWriter.AppendRow(row)
		}

		style.TableWriter.Render()

		fmt.Println()
		fmt.Printf("Use `%s` for more details about a job\n", utils.FullUse(inspectJobCmd))
		fmt.Printf("Use `%s` to list all checkpoints for a job\n", utils.FullUse(listJobCheckpointCmd))

		return nil
	},
}

var killJobCmd = &cobra.Command{
	Use:               "kill <JID>",
	Short:             "Kill a managed process/container (job)",
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: ValidJIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

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
			if !utils.Confirm(cmd.Context(), "Are you sure you want to kill all jobs?") {
				return nil
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
	Use:               "delete <JID>",
	Short:             "Delete a managed process/container (job)",
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: ValidJIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

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
			if !utils.Confirm(cmd.Context(), "Are you sure you want to delete all jobs?") {
				return nil
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
	Use:               "attach <JID>",
	Short:             "Attach stdin/out/err to a managed process/container (job)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: ValidJIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		jid := args[0]

		list, err := client.List(cmd.Context(), &daemon.ListReq{JIDs: []string{jid}})
		if err != nil {
			return err
		}
		if len(list.Jobs) == 0 {
			return fmt.Errorf("Job %s not found", jid)
		}

		pid := list.Jobs[0].GetProcess().GetPID()

		return client.Attach(cmd.Context(), &daemon.AttachReq{PID: pid})
	},
}

var inspectJobCmd = &cobra.Command{
	Use:               "inspect <JID>",
	Short:             "Inspect a managed process/container (job)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: ValidJIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		jid := args[0]

		list, err := client.List(cmd.Context(), &daemon.ListReq{JIDs: []string{jid}})
		if err != nil {
			return err
		}
		if len(list.Jobs) == 0 {
			return fmt.Errorf("Job %s not found", jid)
		}

		job := list.Jobs[0]

		bytes, err := yaml.Marshal(job)
		if err != nil {
			return fmt.Errorf("Error marshalling job: %v", err)
		}

		fmt.Print(string(bytes))

		return nil
	},
}

/////////////////////////////
//// Checkpoint Commands ////
/////////////////////////////

var jobCheckpointCmd = &cobra.Command{
	Use:   "checkpoint",
	Short: "Manage job checkpoints",
}

var listJobCheckpointCmd = &cobra.Command{
	Use:               "list <JID>",
	Short:             "List all checkpoints for a job",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: ValidJIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		jid := args[0]

		list, err := client.List(cmd.Context(), &daemon.ListReq{JIDs: []string{jid}})
		if err != nil {
			return err
		}
		if len(list.Jobs) == 0 {
			return fmt.Errorf("Job %s not found", jid)
		}

		job := list.Jobs[0]

		if len(job.GetCheckpoints()) == 0 {
			fmt.Printf("No checkpoints found for job %s\n", jid)
			return nil
		}

		style.TableWriter.AppendHeader(table.Row{
			"#",
			"Time",
			"Size",
			"Path",
		})

		checkpoints := job.GetCheckpoints()
		sort.Slice(checkpoints, func(i, j int) bool {
			return checkpoints[i].GetTime() > checkpoints[j].GetTime()
		})

		for i, checkpoint := range job.GetCheckpoints() {
			timestamp := time.UnixMilli(checkpoint.GetTime())
			row := table.Row{
				i + 1,
				timestamp.Format(time.DateTime),
				utils.SizeStr(checkpoint.GetSize()),
				checkpoint.GetPath(),
			}
			style.TableWriter.AppendRow(row)
		}

		style.TableWriter.Render()

		fmt.Println()
		fmt.Printf("Use `%s` to inspect a checkpoint.\n", inspectJobCheckpointCmdUse)

		return nil
	},
}

var (
	inspectJobCheckpointCmdUse = "inspect <JID> [checkpoint #]"
	inspectJobCheckpointCmd    = &cobra.Command{
		Use:               inspectJobCheckpointCmdUse,
		Short:             fmt.Sprintf("Inspect a checkpoint for a job. Get checkpoint # from `%s`", utils.FullUse(listJobCheckpointCmd)),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: ValidJIDs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
			if !ok {
				return fmt.Errorf("invalid client in context")
			}

			var info func(path string, imgType string) ([]byte, error)
			features.CheckpointInspect.IfAvailable(func(name string, f func(path string, imgType string) ([]byte, error)) error {
				info = f
				return nil
			})
			if info == nil {
				return fmt.Errorf("Please install a plugin that supports checkpoint inspection. Use `%s` to see available plugins.",
					utils.FullUse(pluginListCmd),
				)
			}

			var index int
			jid := args[0]
			if len(args) < 2 {
				index = 1
			} else {
				index, _ = strconv.Atoi(args[1])
			}
			imgType, _ := cmd.Flags().GetString(flags.TypeFlag.Full)

			list, err := client.List(cmd.Context(), &daemon.ListReq{JIDs: []string{jid}})
			if err != nil {
				return err
			}
			if len(list.Jobs) == 0 {
				return fmt.Errorf("Job %s not found", jid)
			}

			job := list.Jobs[0]

			if len(job.GetCheckpoints()) == 0 {
				fmt.Printf("No checkpoints found for job %s\n", jid)
				return nil
			}

			var checkpointToInspect *daemon.Checkpoint
			for i, cp := range job.GetCheckpoints() {
				if index == i+1 {
					checkpointToInspect = cp
					break
				}
			}

			if checkpointToInspect == nil {
				return fmt.Errorf("Checkpoint %d not found for job %s", index, jid)
			}

			bytes, err := info(checkpointToInspect.GetPath(), imgType)
			if err != nil {
				return err
			}

			fmt.Print(string(bytes))

			return nil
		},
	}
)
