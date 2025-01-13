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
	pluginListCmd.Flags().
		BoolP(flags.AllFlag.Full, flags.AllFlag.Short, false, "List all available plugins")
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
		manager := plugins.NewPropagatorManager(config.Global.Connection)

		ctx := context.WithValue(cmd.Context(), keys.PLUGIN_MANAGER_CONTEXT_KEY, manager)
		cmd.SetContext(ctx)

		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

var pluginListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List plugins",
	Aliases: []string{"ls"},
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, ok := cmd.Context().Value(keys.PLUGIN_MANAGER_CONTEXT_KEY).(plugins.Manager)
		if !ok {
			return fmt.Errorf("failed to get plugin manager")
		}

		all, _ := cmd.Flags().GetBool(flags.AllFlag.Full)

		var status []plugins.Status
		if !all {
			status = []plugins.Status{plugins.Installed, plugins.Available, plugins.Outdated}
		}

		list, err := manager.List(status...)
		if err != nil {
			return err
		}

		if len(list) == 0 {
			if all {
				fmt.Println("No plugins available")
			} else {
				fmt.Println("No plugins installed")
			}
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
			"Latest version",
			"Published",
		})

		tableWriter.SortBy([]table.SortBy{
			{Name: "Status", Mode: table.Asc},
		})

		for _, p := range list {
			row := table.Row{
				p.Name,
				utils.SizeStr(p.Size),
				statusStr(p.Status),
				p.Version,
				p.LatestVersion,
				utils.TimeAgo(p.PublishedAt),
			}
			tableWriter.AppendRow(row)
		}

		tableWriter.Render()

		installedCount := 0
		availableCount := 0
		for _, p := range list {
			if p.Status == plugins.Installed {
				installedCount++
			} else if p.Status == plugins.Available {
				availableCount++
			}
		}

		fmt.Printf("\n%d installed, %d available\n", installedCount, availableCount)

		return nil
	},
}

var pluginInstallCmd = &cobra.Command{
	Use:               "install <plugin>...",
	Short:             "Install a plugin",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: ValidPlugins,
	RunE: func(cmd *cobra.Command, names []string) error {
		if utils.IsRootUser() == false {
			return fmt.Errorf("this command must be run as root")
		}

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
		if utils.IsRootUser() == false {
			return fmt.Errorf("this command must be run as root")
		}

		manager, ok := cmd.Context().Value(keys.PLUGIN_MANAGER_CONTEXT_KEY).(plugins.Manager)
		if !ok {
			return fmt.Errorf("failed to get plugin manager")
		}

		all, _ := cmd.Flags().GetBool(flags.AllFlag.Full)
		if all {
			list, err := manager.List(plugins.Installed)
			if err != nil {
				return err
			}
			args = []string{}
			for _, p := range list {
				args = append(args, p.Name)
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

		list, err := manager.List()
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
			if p.Type == plugins.External {
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
			tableWriter.AppendRow(featureRow(manager, features.HelperCmds, pluginNames, &errs))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.DumpMiddleware, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RestoreMiddleware, pluginNames, &errs))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.RunHandler, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.RunMiddleware, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.ManageHandler, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.KillSignal, pluginNames, &errs))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.GPUInterception, pluginNames, &errs))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.CheckpointInspect, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.CheckpointDecode, pluginNames, &errs))
			tableWriter.AppendRow(featureRow(manager, features.CheckpointEncode, pluginNames, &errs))
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
	case plugins.Available:
		return style.InfoColors.Sprint(s.String())
	case plugins.Installed:
		return style.PositiveColors.Sprint(s.String())
	case plugins.Outdated:
		return style.WarningColors.Sprint(s.String())
	case plugins.Unknown:
		return style.DisabledColors.Sprint(s.String())
	default:
		return s.String()
	}
}
