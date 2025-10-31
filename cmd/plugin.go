package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func init() {
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginInstallCmd)
	pluginCmd.AddCommand(pluginRemoveCmd)
	pluginCmd.AddCommand(pluginFeaturesCmd)

	// Subcommand flags
	pluginRemoveCmd.Flags().
		BoolP(flags.AllFlag.Full, flags.AllFlag.Short, false, "Remove all installed plugins")
	pluginFeaturesCmd.Flags().
		BoolP(flags.ErrorsFlag.Full, flags.ErrorsFlag.Short, false, "Show all errors")

	// Add aliases
	rootCmd.AddCommand(utils.AliasOf(pluginListCmd, "plugins"))
	rootCmd.AddCommand(utils.AliasOf(pluginFeaturesCmd, "features"))
}

// Parent plugin command
var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage plugins",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
		manager := plugins.NewPropagatorManager(config.Global.Connection, rootCmd.Version)

		ctx := context.WithValue(cmd.Context(), keys.PLUGIN_MANAGER_CONTEXT_KEY, manager)
		cmd.SetContext(ctx)

		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

var pluginListCmd = &cobra.Command{
	Use:               "list [plugin]...",
	Short:             "List plugins (specify plugin <name>@<version> to filter)",
	Aliases:           []string{"ls"},
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: ValidPlugins,
	RunE: func(cmd *cobra.Command, names []string) error {
		manager, ok := cmd.Context().Value(keys.PLUGIN_MANAGER_CONTEXT_KEY).(plugins.Manager)
		if !ok {
			return fmt.Errorf("failed to get plugin manager")
		}

		list, err := manager.List(true, names...)
		if err != nil {
			return err
		}

		if len(list) == 0 {
			fmt.Println("No plugins to show")
			return nil
		}
		tableWriter := table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)
		tableWriter.SetOutputMirror(os.Stdout)

		tableWriter.SetStyle(style.TableStyle)
		tableWriter.Style().Options.SeparateRows = false

		tableWriter.AppendHeader(table.Row{
			"Plugin",
			"Size",
			"Status",
			"Installed version",
			"Available version",
			"Published",
		})

		tableWriter.SortBy([]table.SortBy{
			{Name: "Plugin", Mode: table.Asc},
			{Name: "Status", Mode: table.Asc},
		})

		for _, p := range list {
			row := table.Row{
				p.Name,
				utils.SizeStr(p.Size),
				statusStr(p.Status),
				p.Version,
				p.AvailableVersion,
				utils.TimeAgo(p.PublishedAt),
			}
			tableWriter.AppendRow(row)
		}

		tableWriter.Render()

		installedCount := 0
		availableCount := 0
		for _, p := range list {
			if p.Status == plugins.INSTALLED || p.Status == plugins.OUTDATED {
				installedCount++
			} else if p.Status == plugins.AVAILABLE {
				availableCount++
			}
		}

		fmt.Printf("\n%d installed, %d available\n", installedCount, availableCount)

		return nil
	},
}

var pluginInstallCmd = &cobra.Command{
	Use:               "install <plugin>...",
	Short:             "Install a plugin (specify version with <plugin>@<version>)",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: ValidPlugins,
	RunE: func(cmd *cobra.Command, names []string) error {
		manager, ok := cmd.Context().Value(keys.PLUGIN_MANAGER_CONTEXT_KEY).(plugins.Manager)
		if !ok {
			return fmt.Errorf("failed to get plugin manager")
		}

		installed := 0
		anyErrors := false
		install, msgs, errs := manager.Install(names)

		for {
			select {
			case i, ok := <-install:
				if !ok {
					install = nil
					break
				}
				installed += i
			case msg, ok := <-msgs:
				if !ok {
					msgs = nil
					break
				}
				fmt.Println(msg)
			case err, ok := <-errs:
				if !ok {
					errs = nil
					break
				}
				anyErrors = true
				fmt.Println(err)
			}
			if install == nil && msgs == nil && errs == nil {
				break
			}
		}

		if anyErrors && installed < len(names) {
			return fmt.Errorf("Installed %d plugin(s), %d failed", installed, len(names)-installed)
		} else {
			fmt.Printf("Installed %d plugin(s)\n", installed)
			return nil
		}
	},
}

var pluginRemoveCmd = &cobra.Command{
	Use:               "remove <plugin>...",
	Short:             "Remove a plugin",
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: ValidPlugins,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, ok := cmd.Context().Value(keys.PLUGIN_MANAGER_CONTEXT_KEY).(plugins.Manager)
		if !ok {
			return fmt.Errorf("failed to get plugin manager")
		}

		all, _ := cmd.Flags().GetBool(flags.AllFlag.Full)
		if all {
			list, err := manager.List(false)
			if err != nil {
				return err
			}
			args = []string{}
			for _, p := range list {
				if p.Status == plugins.INSTALLED {
					args = append(args, p.Name)
				}
			}
		} else if len(args) == 0 {
			return fmt.Errorf("specify at least one plugin to remove or use --all")
		}

		removed := 0
		remove, msgs, errs := manager.Remove(args)

		for {
			select {
			case i, ok := <-remove:
				if !ok {
					remove = nil
					break
				}
				removed += i
			case msg, ok := <-msgs:
				if !ok {
					msgs = nil
					break
				}
				fmt.Println(msg)
			case err, ok := <-errs:
				if !ok {
					errs = nil
					break
				}
				fmt.Println(err)
			}
			if remove == nil && msgs == nil && errs == nil {
				break
			}
		}

		if removed < len(args) {
			return fmt.Errorf("Removed %d plugins, %d failed", removed, len(args)-removed)
		} else {
			fmt.Printf("Removed %d plugins\n", removed)
			return nil
		}
	},
}

var pluginFeaturesCmd = &cobra.Command{
	Use:               "features [plugin]...",
	Short:             "Show feature matrix of plugins",
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: ValidPlugins,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, ok := cmd.Context().Value(keys.PLUGIN_MANAGER_CONTEXT_KEY).(plugins.Manager)
		if !ok {
			return fmt.Errorf("failed to get plugin manager")
		}

		list, err := manager.List(false)
		if err != nil {
			return err
		}

		filter := make(map[string]struct{})
		for _, plugin := range args {
			filter[plugin] = struct{}{}
		}

		// filter the list
		if len(filter) > 0 {
			var newList []plugins.Plugin
			for _, p := range list {
				if _, ok := filter[p.Name]; ok {
					newList = append(newList, p)
				}
			}
			list = newList
		}

		if len(list) == 0 {
			fmt.Println("No plugins available")
			return nil
		}

		tableWriter := table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)
		tableWriter.SetOutputMirror(os.Stdout)

		tableWriter.SetStyle(style.TableStyle)
		tableWriter.Style().Options.SeparateRows = false

		header := table.Row{
			"Feature",
		}

		var pluginNames []string
		var externalPlugins []string
		for _, p := range list {
			if p.Type == plugins.EXTERNAL {
				externalPlugins = append(externalPlugins, p.Name)
				continue
			}
			pluginNames = append(pluginNames, p.Name)
			header = append(header, p.Name)
		}

		errs := []error{}

		if len(pluginNames) > 0 {
			tableWriter.AppendHeader(header)
			tableWriter.AppendRow(featureRow(manager, features.DumpCmd, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RestoreCmd, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RunCmd, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.ManageCmd, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.FreezeCmd, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.UnfreezeCmd, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.QueryCmd, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.HelperCmds, pluginNames, &errs))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.DumpMiddleware, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.DumpHandler, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.DumpVMMiddleware, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.DumpVMHandler, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RestoreMiddleware, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RestoreMiddlewareLate, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RestoreHandler, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RestoreVMMiddleware, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RestoreVMHandler, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.FreezeHandler, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.UnfreezeHandler, pluginNames, &errs))

			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.RunHandler, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RunDaemonlessSupport, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RunMiddleware, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RunMiddlewareLate, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.ManageHandler, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.KillSignal, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.Cleanup, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.Reaper, pluginNames, &errs))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.GPUInterception, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.GPUInterceptionRestore, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.GPUTracing, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.GPUTracingRestore, pluginNames, &errs))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.Storage, pluginNames, &errs))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.QueryHandler, pluginNames, &errs))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.HealthChecks, pluginNames, &errs))

			tableWriter.Render()
			fmt.Println()
			fmt.Println(featureLegend())
		}

		if len(externalPlugins) > 0 {
			fmt.Printf("Not showing external plugins: %s\n", utils.StrList(externalPlugins))
		}

		showErrors, _ := cmd.Flags().GetBool(flags.ErrorsFlag.Full)

		if showErrors && len(errs) > 0 {
			fmt.Println()
			for _, err := range errs {
				fmt.Println(style.NegativeColors.Sprint(err))
			}
			return fmt.Errorf("Found %d issue(s). Try updating.", len(errs))
		}

		return nil
	},
}

////////////////////
/// Helper Funcs ///
////////////////////

func featureRow[T any](manager plugins.Manager, feature plugins.Feature[T], pluginNames []string, errs *[]error) table.Row {
	row := table.Row{feature.Description}

	for _, name := range pluginNames {
		if manager.IsInstalled(name) == false {
			row = append(row, style.DisabledColors.Sprint(style.DashMark))
			continue
		}
		available, err := feature.IsAvailable(name)
		if err != nil {
			*errs = append(*errs, err)
			row = append(row, style.NegativeColors.Sprint(style.CrossMark))
		} else {
			row = append(row, style.BoolStr(available, style.TickMark, style.BulletMark))
		}
	}

	return row
}

func featureLegend() string {
	return fmt.Sprintf("%s = implemented, %s = unimplemented, %s = not installed, %s = incompatible",
		style.PositiveColors.Sprint(style.TickMark),
		style.DisabledColors.Sprint(style.BulletMark),
		style.DisabledColors.Sprint(style.DashMark),
		style.NegativeColors.Sprint(style.CrossMark))
}

func statusStr(s plugins.Status) string {
	switch s {
	case plugins.AVAILABLE:
		return style.InfoColors.Sprint(s.String())
	case plugins.INSTALLED:
		return style.PositiveColors.Sprint(s.String())
	case plugins.OUTDATED:
		return style.WarningColors.Sprint(s.String())
	case plugins.UNKNOWN:
		return style.DisabledColors.Sprint(s.String())
	default:
		return s.String()
	}
}
