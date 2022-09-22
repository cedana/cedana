package cmd

import (
	"fmt"
	"os/exec"
	"regexp"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/spf13/cobra"
)

var container string

func init() {
	dockerCmd.AddCommand(dockerDumpCmd)
	dockerDumpCmd.Flags().StringVarP(&dir, "dir", "d", "", "folder to dump checkpoint into")
	dockerDumpCmd.Flags().IntVarP(&pid, "container", "c", 0, "container to dump")
}

// This inherits the EXPERIMENTAL tag from the docs https://docs.docker.com/engine/reference/commandline/checkpoint/,
// even though it uses CRIU behind the scenes.

// Need to have experimental features enabled for this to work, something like:
/// echo "{\"experimental\": true}" >> /etc/docker/daemon.json
/// systemctl restart docker

var dockerDumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Directly dump a docker container to disk",
	RunE: func(cmd *cobra.Command, args []string) error {
		dc, err := instantiateDockerClient()
		if err != nil {
			return err
		}

		if dir == "" {
			dir = dc.config.Client.DumpStorageDir
		}

		opts := dockertypes.CheckpointCreateOptions{
			CheckpointID:  "checkpoint", // TODO: generate uuid based on container name?
			CheckpointDir: dir,
			Exit:          false, // leaves container running
		}
		err = dc.Docker.CheckpointCreate(cmd.Context(), container, opts)
		if err != nil {
			dc.logger.Fatal().Err(err).Msg("Docker checkpoint dump failed")
			// yoinking what the docker sdk does here with the dumplog because there's currently no way to configure where it goes
			re := regexp.MustCompile("path= (.*): ")
			m := re.FindStringSubmatch(fmt.Sprintf("%s", err))
			if len(m) >= 2 {
				dumpLog := m[1]
				dc.logger.Info().Msgf("%s", dumpLog)
				cmd := exec.Command("cat", dumpLog)
				stdoutStderr, _ := cmd.CombinedOutput()
				dc.logger.Fatal().Msgf("%s", stdoutStderr)
			}
		}
		return nil
	},
}
