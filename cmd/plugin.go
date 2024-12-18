package cmd

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/features"
	"github.com/cedana/cedana/pkg/config"
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

	// Add aliases
	pluginCmd.AddCommand(utils.AliasOf(pluginListCmd, "ls"))
	rootCmd.AddCommand(utils.AliasOf(pluginListCmd, "plugins"))
	rootCmd.AddCommand(utils.AliasOf(pluginFeaturesCmd, "features"))
}

// Parent plugin command
var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage plugins",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
		manager := plugins.NewLocalManager()

		useVSOCK := config.Global.UseVSOCK
		var client *Client

		if useVSOCK {
			client, err = NewVSOCKClient(config.Global.ContextID, config.Global.Port)
		} else {
			client, err = NewClient(config.Global.Host, config.Global.Port)
		}

		ctx := context.WithValue(cmd.Context(), keys.PLUGIN_MANAGER_CONTEXT_KEY, manager)
		ctx = context.WithValue(ctx, keys.CLIENT_CONTEXT_KEY, client)
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

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List plugins",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, ok := cmd.Context().Value(keys.PLUGIN_MANAGER_CONTEXT_KEY).(plugins.Manager)
		if !ok {
			return fmt.Errorf("failed to get plugin manager")
		}

		all, _ := cmd.Flags().GetBool(flags.AllFlag.Full)

		var status []plugins.Status
		if !all {
			status = []plugins.Status{plugins.Installed, plugins.Available}
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
			"Dependencies",
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
				utils.StrList(p.Dependencies),
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

		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}
		manager, ok := cmd.Context().Value(keys.PLUGIN_MANAGER_CONTEXT_KEY).(plugins.Manager)
		if !ok {
			return fmt.Errorf("failed to get plugin manager")
		}

		installed := 0
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
				fmt.Println(err)
			}
			if install == nil && msgs == nil && errs == nil {
				break
			}
		}

		// Tell daemon to reload plugins
		client.ReloadPlugins(cmd.Context(), &daemon.Empty{})

		if installed < len(names) {
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

		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
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

		// Tell daemon to reload plugins
		client.ReloadPlugins(cmd.Context(), &daemon.Empty{})

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

		if len(pluginNames) > 0 {
			tableWriter.AppendHeader(header)
			tableWriter.AppendRow(featureRow(manager, features.DumpCmd, pluginNames))
			tableWriter.AppendRow(featureRow(manager, features.RestoreCmd, pluginNames))
			tableWriter.AppendRow(featureRow(manager, features.RunCmd, pluginNames))
			tableWriter.AppendRow(featureRow(manager, features.RootCmds, pluginNames))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.DumpMiddleware, pluginNames))
			tableWriter.AppendRow(featureRow(manager, features.RestoreMiddleware, pluginNames))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.RunHandler, pluginNames))
			tableWriter.AppendRow(featureRow(manager, features.RunMiddleware, pluginNames))
			tableWriter.AppendRow(featureRow(manager, features.GPUInterception, pluginNames))
			tableWriter.AppendRow(featureRow(manager, features.KillSignal, pluginNames))
			tableWriter.AppendSeparator()
			tableWriter.AppendRow(featureRow(manager, features.CheckpointInspect, pluginNames))
			tableWriter.AppendRow(featureRow(manager, features.CheckpointDecode, pluginNames))
			tableWriter.AppendRow(featureRow(manager, features.CheckpointEncode, pluginNames))

			tableWriter.Render()
			fmt.Println()
			fmt.Println(featureLegend())
		}

		if len(externalPlugins) > 0 {
			fmt.Printf("Not showing external plugins: %s\n", utils.StrList(externalPlugins))
		}

		return nil
	},
}

////////////////////
/// Helper Funcs ///
////////////////////

func featureRow[T any](manager plugins.Manager, feature plugins.Feature[T], pluginNames []string) table.Row {
	row := table.Row{feature.Description}

	for _, name := range pluginNames {
		if manager.IsInstalled(name) == false {
			row = append(row, style.DisbledColor.Sprint("-"))
			continue
		}
		available, err := feature.IsAvailable(name)
		if err != nil {
			row = append(row, style.NegativeColor.Sprint("!"))
		} else {
			row = append(row, style.BoolStr(available, "✔", "✘"))
		}
	}

	return row
}

func featureLegend() string {
	return fmt.Sprintf("%s = supported, %s = unsupported, %s = not installed, %s = incompatible",
		style.PositiveColor.Sprint("✔"),
		style.DisbledColor.Sprint("✘"),
		style.DisbledColor.Sprint("-"),
		style.NegativeColor.Sprint("!"))
}

func statusStr(s plugins.Status) string {
	switch s {
	case plugins.Available:
		return style.WarningColor.Sprint(s.String())
	case plugins.Installed:
		return style.PositiveColor.Sprint(s.String())
	case plugins.Unknown:
		return style.DisbledColor.Sprint(s.String())
	default:
		return s.String()
	}
}
