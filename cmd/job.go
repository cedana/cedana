package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/xeonx/timeago"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/cedana/go-criu/v7/crit"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
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
	listJobCmd.Flags().BoolP(flags.AllFlag.Full, flags.AllFlag.Short, false, "include jobs from remote hosts")
	deleteJobCmd.Flags().BoolP(flags.AllFlag.Full, flags.AllFlag.Short, false, "delete all jobs")
	killJobCmd.Flags().BoolP(flags.AllFlag.Full, flags.AllFlag.Short, false, "kill all jobs")
	inspectJobCheckpointCmd.Flags().StringP(flags.TypeFlag.Full, flags.TypeFlag.Short, "", "specify image file {ps|fd|mem|rss|sk|gpu}")

	// Add aliases
	jobCmd.AddCommand(utils.AliasOf(listJobCheckpointCmd, "checkpoints"))
	rootCmd.AddCommand(utils.AliasOf(listJobCmd, "ps"))
	rootCmd.AddCommand(utils.AliasOf(listJobCmd, "jobs"))
	rootCmd.AddCommand(utils.AliasOf(deleteJobCmd))
	rootCmd.AddCommand(utils.AliasOf(killJobCmd))
	rootCmd.AddCommand(utils.AliasOf(jobCheckpointCmd))
	rootCmd.AddCommand(utils.AliasOf(listJobCheckpointCmd, "checkpoints"))
}

// Parent job command
var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage jobs",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
		client, err := client.New(config.Global.Address, config.Global.Protocol)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}

		ctx := context.WithValue(cmd.Context(), keys.CLIENT_CONTEXT_KEY, client)
		cmd.SetContext(ctx)

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
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
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all managed processes/containers (jobs)",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		req := &daemon.ListReq{}

		all, _ := cmd.Flags().GetBool(flags.AllFlag.Full)
		if all {
			req.Remote = true
		}

		resp, err := client.List(cmd.Context(), req)
		if err != nil {
			return err
		}
		jobs := resp.Jobs

		if len(jobs) == 0 {
			fmt.Println("No jobs found")
			return nil
		}

		tableWriter := table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)
		tableWriter.SetOutputMirror(os.Stdout)
		tableWriter.SetColumnConfigs([]table.ColumnConfig{
			{Name: "PID", Align: text.AlignRight, AlignHeader: text.AlignRight, AlignFooter: text.AlignRight},
			{Name: "Size", Align: text.AlignRight, AlignHeader: text.AlignRight, AlignFooter: text.AlignRight},
			{Name: "Checkpoint", Align: text.AlignRight, AlignHeader: text.AlignRight, AlignFooter: text.AlignRight},
			{Name: "Log", Align: text.AlignLeft, AlignHeader: text.AlignLeft, AlignFooter: text.AlignLeft},
		})

		tableWriter.AppendHeader(table.Row{
			"Job",
			"Type",
			"PID",
			"Status",
			"GPU",
			"Checkpoint",
			"Size",
			"Log",
		})

		tableWriter.SortBy([]table.SortBy{
			{Name: "Status", Mode: table.Dsc},
			{Name: "Checkpoint"},
		})

		statusStr := func(status string) string {
			switch status {
			case "running", "sleep":
				return style.PositiveColors.Sprint(status)
			case "zombie":
				return style.WarningColors.Sprint(status)
			case "remote":
				return style.InfoColors.Sprint(status)
			case "halted":
				return style.DisabledColors.Sprint(status)
			}
			return style.DisabledColors.Sprint(status)
		}

		// Color type based on the plugin theme
		typeStr := func(t string) string {
			colorToUse := text.Colors{}
			features.CmdTheme.IfAvailable(func(name string, theme text.Colors) error {
				colorToUse = theme
				return nil
			}, t)
			return colorToUse.Sprint(t)
		}

		var timeList []string
		var sizeList []string
		for _, job := range jobs {
			resp, err := client.GetCheckpoint(cmd.Context(), &daemon.GetCheckpointReq{JID: proto.String(job.JID)})
			if err != nil {
				return err
			}
			checkpoint := resp.Checkpoint

			if checkpoint == nil {
				timeList = append(timeList, "")
				sizeList = append(sizeList, "")
			} else {
				latestTime := timeago.NoMax(timeago.English).Format(time.UnixMilli(checkpoint.GetTime()))
				latestSize := utils.SizeStr(checkpoint.GetSize())
				timeList = append(timeList, latestTime)
				sizeList = append(sizeList, latestSize)
			}
		}

		for i, job := range jobs {
			row := table.Row{
				job.GetJID(),
				typeStr(job.GetType()),
				job.GetState().GetPID(),
				statusStr(job.GetState().GetStatus()),
				style.BoolStr(job.GetState().GetGPUEnabled()),
				timeList[i],
				sizeList[i],
				job.GetLog(),
			}
			tableWriter.AppendRow(row)
		}

		tableWriter.Render()

		fmt.Println()
		fmt.Printf("Use `%s` for more details about a job\n", utils.FullUse(inspectJobCmd))
		fmt.Printf("Use `%s` to list all checkpoints for a job\n", utils.FullUse(listJobCheckpointCmd))

		return nil
	},
}

var killJobCmd = &cobra.Command{
	Use:               "kill <JID>...",
	Short:             "Kill a managed process/container (job)",
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: RunningJIDs,
	RunE: func(cmd *cobra.Command, jids []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		req := &daemon.KillReq{}

		if len(jids) > 0 {
			req.JIDs = jids
		} else {
			// Check if the all flag is set
			all, _ := cmd.Flags().GetBool(flags.AllFlag.Full)
			if !all {
				return fmt.Errorf("Please provide at least one JID or use the --all flag")
			}
			if !utils.Confirm(cmd.Context(), "Are you sure you want to kill all jobs?") {
				return nil
			}
		}

		resp, err := client.Kill(cmd.Context(), req)

		for _, message := range resp.GetMessages() {
			fmt.Println(message)
		}

		return err
	},
}

var deleteJobCmd = &cobra.Command{
	Use:               "delete <JID>...",
	Short:             "Delete a managed process/container (job)",
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: ValidJIDs,
	RunE: func(cmd *cobra.Command, jids []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		req := &daemon.DeleteReq{}

		if len(jids) > 0 {
			req.JIDs = jids
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

		resp, err := client.Delete(cmd.Context(), req)

		for _, message := range resp.GetMessages() {
			fmt.Println(message)
		}

		return err
	},
}

var attachJobCmd = &cobra.Command{
	Use:               "attach <JID>",
	Short:             "Attach stdin/out/err to a managed process/container (job)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: RunningJIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		jid := args[0]

		resp, err := client.Get(cmd.Context(), &daemon.GetReq{JID: jid})
		if err != nil {
			return err
		}

		job := resp.GetJob()

		pid := job.GetState().GetPID()

		return client.Attach(cmd.Context(), &daemon.AttachReq{PID: pid})
	},
}

var inspectJobCmd = &cobra.Command{
	Use:               "inspect <JID>",
	Short:             "Inspect a managed process/container (job)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: ValidJIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		jid := args[0]

		resp, err := client.Get(cmd.Context(), &daemon.GetReq{JID: jid})
		if err != nil {
			return err
		}

		job := resp.GetJob()

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
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		jid := args[0]

		resp, err := client.ListCheckpoints(cmd.Context(), &daemon.ListCheckpointsReq{JID: jid})
		if err != nil {
			return err
		}

		if len(resp.Checkpoints) == 0 {
			fmt.Printf("No checkpoints found for job %s\n", jid)
			return nil
		}

		tableWriter := table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)
		tableWriter.SetOutputMirror(os.Stdout)

		tableWriter.AppendHeader(table.Row{
			"ID",
			"Time",
			"Size",
			"Path",
		})

		checkpoints := resp.GetCheckpoints()
		sort.Slice(checkpoints, func(i, j int) bool {
			return checkpoints[i].GetTime() > checkpoints[j].GetTime()
		})

		for _, checkpoint := range checkpoints {
			timestamp := time.UnixMilli(checkpoint.GetTime())
			row := table.Row{
				checkpoint.GetID(),
				timestamp.Format(time.DateTime),
				utils.SizeStr(checkpoint.GetSize()),
				checkpoint.GetPath(),
			}
			tableWriter.AppendRow(row)
		}

		tableWriter.Render()

		fmt.Println()
		fmt.Printf("Use `%s` to inspect a checkpoint.\n", inspectJobCheckpointCmdUse)

		return nil
	},
}

var (
	inspectJobCheckpointCmdUse = "inspect <checkpoint-id>"
	inspectJobCheckpointCmd    = &cobra.Command{
		Use:   inspectJobCheckpointCmdUse,
		Short: "Inspect a checkpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
			if !ok {
				return fmt.Errorf("invalid client in context")
			}

			id := args[0]

			resp, err := client.GetCheckpoint(cmd.Context(), &daemon.GetCheckpointReq{ID: proto.String(id)})
			if err != nil {
				return err
			}

			image := crit.New(nil, nil, resp.GetCheckpoint().GetPath(), true, true)

			fds, err := image.ExploreFds()
			if err != nil {
				return err
			}

			for _, fd := range fds {
				for _, file := range fd.Files {
					fmt.Println(file.Path)
				}
			}

			// TODO: Move checkpoint inespection to daemon, to allow inspecting compressed or even streamable checkpoints

			return nil
		},
	}
)
