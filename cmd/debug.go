package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var debugCmd = &cobra.Command{
	Use:    "debug",
	Short:  "Functions/tools for debugging instances or testing new components",
	Hidden: true,
}

var cfgCmd = &cobra.Command{
	Use: "config",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := utils.GetConfig()
		if err != nil {
			return err
		}
		fmt.Printf("config file used: %s", viper.GetViper().ConfigFileUsed())
		// pretty print config for debugging to make sure it's been loaded correctly
		prettyCfg, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(prettyCfg))
		return nil
	},
}

var compressCmd = &cobra.Command{
	Use:  "compress",
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return utils.TarGzFolder(args[0], args[1])
	},
}

var decompressCmd = &cobra.Command{
	Use:  "decompress",
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return utils.UntarGzFolder(args[0], args[1])
	},
}

// experimental, testing out debugging the container checkpointing
var containerCmd = &cobra.Command{
	Use: "container",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := args[0]
		id := args[1]

		err := container.Restore(dir, id)
		if err != nil {
			return err
		}

		return nil
	},
}

var debugRuncRestoreCmd = &cobra.Command{
	Use: "runc-restore",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Typical routes
		// root := "/var/run/runc"
		// bundle := "$HOME/bundle"
		// consoleSocket := "/home/brandonsmith/tty.sock"
		root := args[2]
		bundle := args[3]
		// consoleSocket := args[4]
		opts := &container.RuncOpts{
			Root:    root,
			Bundle:  bundle,
			Detatch: false,
		}
		imgPath := args[0]
		containerId := args[1]

		client := api.Client{}

		err := client.RuncRestore(cmd.Context(), imgPath, containerId, false, []string{}, opts)
		if err != nil {
			return err
		}

		return nil
	},
}

var debugRuncDumpCmd = &cobra.Command{
	Use: "runc-dump",
	RunE: func(cmd *cobra.Command, args []string) error {
		imgPath := args[0]
		containerId := args[1]
		root := "/var/run/runc"

		client := api.Client{}

		criuOpts := &container.CriuOpts{
			ImagesDirectory: imgPath,
			WorkDirectory:   "",
			LeaveRunning:    true,
			TcpEstablished:  false,
		}

		client.RuncDump(cmd.Context(), root, containerId, criuOpts)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(debugCmd)
	debugCmd.AddCommand(cfgCmd)
	debugCmd.AddCommand(containerCmd)
	debugCmd.AddCommand(debugRuncRestoreCmd)
	debugCmd.AddCommand(debugRuncDumpCmd)
	debugCmd.AddCommand(compressCmd)
	debugCmd.AddCommand(decompressCmd)
}
