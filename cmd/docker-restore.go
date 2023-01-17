package cmd

import (
	"fmt"
	"os/exec"
	"regexp"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/spf13/cobra"
)

func init() {
	dockerCmd.AddCommand(dockerRestoreCmd)
	dockerRestoreCmd.Flags().StringVarP(&dir, "dir", "d", "", "folder to dump checkpoint into")
	dockerRestoreCmd.Flags().StringVarP(&container, "container", "c", "", "container to dump")
}

// This inherits the EXPERIMENTAL tag from the docs https://docs.docker.com/engine/reference/commandline/checkpoint/,
// even though it uses CRIU behind the scenes.

var dockerRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore a docker checkpoint",
	RunE: func(cmd *cobra.Command, args []string) error {
		dc, err := instantiateDockerClient()
		if err != nil {
			return err
		}

		if dir == "" {
			dir = dc.config.SharedStorage.DumpStorageDir
		}

		opts := dockertypes.ContainerStartOptions{
			CheckpointID:  "checkpoint",
			CheckpointDir: dir,
		}
		// The way checkpointing is orchestrated within docker
		err = dc.Docker.ContainerStart(cmd.Context(), container, opts)
		if err != nil {
			dc.logger.Fatal().Err(err).Msg("Docker checkpoint restore failed")

			re := regexp.MustCompile("path= (.*): ")
			m := re.FindStringSubmatch(fmt.Sprintf("%s", err))
			if len(m) >= 2 {
				restoreLog := m[1]
				dc.logger.Info().Msgf("%s", restoreLog)
				cmd := exec.Command("cat", restoreLog)
				stdoutStderr, _ := cmd.CombinedOutput()
				dc.logger.Fatal().Msgf("%s", stdoutStderr)
			}
		}
		return nil
	},
}
