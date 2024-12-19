package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/features"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

func init() {
	runCmd.AddCommand(processRunCmd)

	// Add common flags
	runCmd.PersistentFlags().StringP(flags.JidFlag.Full, flags.JidFlag.Short, "", "job id")
	runCmd.PersistentFlags().
		BoolP(flags.GpuEnabledFlag.Full, flags.GpuEnabledFlag.Short, false, "enable GPU support")
	runCmd.PersistentFlags().
		BoolP(flags.AttachFlag.Full, flags.AttachFlag.Short, false, "attach stdin/out/err")
	runCmd.PersistentFlags().
		BoolP(flags.AttachableFlag.Full, flags.AttachableFlag.Short, false, "make it attachable, but don't attach")
	runCmd.PersistentFlags().
		StringP(flags.LogFlag.Full, flags.LogFlag.Short, "", "log path to forward stdout/err")
	runCmd.MarkFlagsMutuallyExclusive(
		flags.AttachFlag.Full,
		flags.LogFlag.Full,
	) // only one of these can be set

	// Add aliases
	rootCmd.AddCommand(utils.AliasOf(processRunCmd, "exec"))

	///////////////////////////////////////////
	// Add modifications from supported plugins
	///////////////////////////////////////////

	features.RunCmd.IfAvailable(
		func(name string, pluginCmd *cobra.Command) error {
			runCmd.AddCommand(pluginCmd)
			return nil
		},
	)
}

// Parent run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a managed process/container (create a job)",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		jid, _ := cmd.Flags().GetString(flags.JidFlag.Full)
		gpuEnabled, _ := cmd.Flags().GetBool(flags.GpuEnabledFlag.Full)
		log, _ := cmd.Flags().GetString(flags.LogFlag.Full)
		attach, _ := cmd.Flags().GetBool(flags.AttachFlag.Full)
		attachable, _ := cmd.Flags().GetBool(flags.AttachableFlag.Full)

		// Create half-baked request
		req := &daemon.RunReq{
			JID:        jid,
			Log:        log,
			GPUEnabled: gpuEnabled,
			Attachable: attach || attachable,
		}

		ctx := context.WithValue(cmd.Context(), keys.RUN_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},

	//******************************************************************************************
	// Let subcommands (incl. from plugins) add details to the request, in the `RunE` hook.
	// Finally, we send the request to the server in the PersistentPostRun hook.
	// The server will make sure to handle it appropriately using any required plugins.
	//******************************************************************************************

	PersistentPostRunE: func(cmd *cobra.Command, args []string) (err error) {
		useVSOCK := config.Global.UseVSOCK
		var client *Client

		if useVSOCK {
			client, err = NewVSOCKClient(config.Global.ContextID, config.Global.Port)
		} else {
			client, err = NewClient(config.Global.Host, config.Global.Port)
		}
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}
		defer client.Close()

		// Assuming request is now ready to be sent to the server
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid request in context")
		}

		resp, err := client.Run(cmd.Context(), req)
		if err != nil {
			return err
		}

		if config.Global.Profiling.Enabled && resp.Profiling != nil {
			printProfilingData(resp.Profiling)
		}

		attach, _ := cmd.Flags().GetBool(flags.AttachFlag.Full)
		if attach {
			return client.Attach(cmd.Context(), &daemon.AttachReq{PID: resp.PID})
		}

		fmt.Printf("Running managed %s PID %d\n", req.Type, resp.PID)

		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

var processRunCmd = &cobra.Command{
	Use:   "process <path> [args...]",
	Short: "Run a managed process (job)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid request in context")
		}

		path := args[0]
		args = args[1:]
		env := os.Environ()
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("Error getting working directory: %v", err)
		}
		groups, err := os.Getgroups()
		if err != nil {
			return fmt.Errorf("Error getting groups: %v", err)
		}

		req.Type = "process"
		req.Details = &daemon.Details{
			Process: &daemon.Process{
				Path:       path,
				Args:       args,
				Env:        env,
				WorkingDir: wd,
				UID:        int32(os.Getuid()),
				GID:        int32(os.Getgid()),
				Groups:     utils.Int32Slice(groups),
			},
		}

		ctx := context.WithValue(cmd.Context(), keys.RUN_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

////////////////////
/// Helper Funcs ///
////////////////////

// PrintProfilingData prints the profiling data in a very readable format.
func printProfilingData(data *daemon.ProfilingData) {
	total := time.Duration(data.Duration)

	profiling.CleanData(data)
	profiling.FlattenData(data) // Since we want to print as a list

	fmt.Print("Profiling data received (flattened):\n\n")

	tableWriter := table.NewWriter()
	tableWriter.SetStyle(style.TableStyle)
	tableWriter.SetOutputMirror(os.Stdout)

	categoryMap := make(map[string]time.Duration)

	remainingDuration := total
	for _, p := range data.Components {
		if p.Duration <= 0 {
			continue
		}
		categoryName, name := utils.SimplifyFuncName(p.Name)

		category := style.WarningColor.Sprint(categoryName)
		features.CmdTheme.IfAvailable(func(name string, theme text.Colors) error {
			category = theme.Sprint(categoryName)
			return nil
		}, categoryName)

		duration := time.Duration(p.Duration)
		remainingDuration -= duration

		if categoryName != "" {
			categoryMap[category] += duration
		} else {
			categoryMap[style.DisbledColor.Sprint("other")] += duration
		}

		tableWriter.AppendRow([]interface{}{duration, category, style.DisbledColor.Sprint(name)})
	}

	tableWriter.AppendFooter([]interface{}{total, "", fmt.Sprintf("%s (total)", data.Name)})
	tableWriter.Render()

	if len(categoryMap) > 1 {
		fmt.Println()
		tableWriter = table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)
		tableWriter.SetOutputMirror(os.Stdout)

		categoryMap[string(style.DisbledColor.Sprint("other"))] += remainingDuration

		for category, duration := range categoryMap {
			percentage := (float64(duration) / float64(total)) * 100
			tableWriter.AppendRow([]interface{}{duration, fmt.Sprintf("%.2f%%", percentage), category})
		}

		tableWriter.AppendFooter([]interface{}{total, "", fmt.Sprintf("%s (total)", data.Name)})
		tableWriter.Render()
	}

	fmt.Println()
}
