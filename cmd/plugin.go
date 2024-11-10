package cmd

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/internal/plugins"
	"github.com/cedana/cedana/pkg/api/style"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func init() {
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginInstallCmd)
	pluginCmd.AddCommand(pluginRemoveCmd)

	// Subcommand flags
	pluginListCmd.Flags().BoolP(types.AllFlag.Full, types.AllFlag.Short, false, "List all available plugins")
}

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage plugins",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if utils.IsRootUser() == false {
			return fmt.Errorf("plugin commands must be run as root")
		}

		manager := plugins.NewLocalManager()
		ctx := context.WithValue(cmd.Context(), types.PLUGIN_MANAGER_CONTEXT_KEY, manager)
		cmd.SetContext(ctx)

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
		manager, ok := cmd.Context().Value(types.PLUGIN_MANAGER_CONTEXT_KEY).(plugins.Manager)
		if !ok {
			return fmt.Errorf("failed to get plugin manager")
		}

		all, _ := cmd.Flags().GetBool(types.AllFlag.Full)

		var status []plugins.Status
		if !all {
			status = []plugins.Status{plugins.Installed}
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

		writer := table.NewWriter()
		writer.SetOutputMirror(cmd.OutOrStdout())
		writer.SetStyle(style.TableStyle)
		writer.Style().Options.SeparateRows = false

		writer.AppendHeader(table.Row{
			"Plugin",
			"Size",
			"Status",
			"Version",
			"Latest Version",
		})

		sizeMibStr := func(bytes int64) string {
			if bytes == 0 {
				return "-"
			}
			return fmt.Sprintf("%d MiB", utils.Mebibytes(bytes))
		}

		statusStr := func(s plugins.Status) string {
			switch s {
			case plugins.Available:
				return style.WarningColor.Sprint(s.String())
			case plugins.Installed:
				return style.PositiveColor.Sprint(s.String())
			default:
				return s.String()
			}
		}

		for _, p := range list {
			row := table.Row{
				p.Name,
				sizeMibStr(p.Size),
				statusStr(p.Status),
				p.Version,
				p.LatestVersion,
			}
			writer.AppendRow(row)
		}

		writer.Render()

		installedCount := 0
		availableCount := 0
		for _, p := range list {
			if p.Status == plugins.Installed {
				installedCount++
			} else if p.Status == plugins.Available {
				availableCount++
			}
		}

		fmt.Printf("%d installed, %d available\n", installedCount, availableCount)

		return nil
	},
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install <plugin>...",
	Short: "Install a plugin",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, names []string) error {
		manager, ok := cmd.Context().Value(types.PLUGIN_MANAGER_CONTEXT_KEY).(plugins.Manager)
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

		if installed < len(names) {
			return fmt.Errorf("Installed %d plugins, %d failed", installed, len(names)-installed)
		} else {
			fmt.Printf("Installed %d plugins\n", installed)
			return nil
		}
	},
}

var pluginRemoveCmd = &cobra.Command{
	Use:   "remove <plugin>...",
	Short: "Remove a plugin",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, ok := cmd.Context().Value(types.PLUGIN_MANAGER_CONTEXT_KEY).(plugins.Manager)
		if !ok {
			return fmt.Errorf("failed to get plugin manager")
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
