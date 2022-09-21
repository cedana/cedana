package cmd

import (
	"github.com/spf13/cobra"
)

func init() {
	clientCommand.AddCommand(dockerDumpCommand)
	dockerDumpCommand.Flags().StringVarP(&dir, "dir", "d", "", "folder to dump checkpoint into")
	dockerDumpCommand.Flags().IntVarP(&pid, "pid", "p", 0, "pid to dump")
}

// This inherits the EXPERIMENTAL tag from the docs https://docs.docker.com/engine/reference/commandline/checkpoint/,
// even though it uses CRIU behind the scenes.

// Need to have experimental features enabled for this to work (TODO: Add check for that)

var dockerDumpCommand = &cobra.Command{
	Use:   "dump",
	Short: "Directly dump a docker container to disk",
	RunE: func(cmd *cobra.Command, args []string) error {
		dc, err := instantiateDockerClient()
		if err != nil {
			return err
		}

		// get flag (container name)
		// get keepRunning opt from config 
		return nil
	},
}
