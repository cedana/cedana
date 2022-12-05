package cmd

import (
	"fmt"
	"os/exec"

	"github.com/nravic/cedana/utils"
	"github.com/spf13/cobra"
)

var setupCommand = &cobra.Command{
	Use:   "setup",
	Short: "Commands to set up an instantiated remote instance for Cedana",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("error: must also specify setup subcommands")
	},
}

var awsCommand = &cobra.Command{
	Use:   "aws",
	Short: "Command for AWS-specific setup",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("error: must also specifcy aws subcommands")
	},
}

var efsAttachCommand = &cobra.Command{
	Use:   "efs-attach",
	Short: "Attach an EFS volume to an EC2 instance. Must have installed efs-utils!",
	RunE: func(cmd *cobra.Command, args []string) error {
		// verify that we're in the correct region?
		// instantiate logger
		logger := utils.GetLogger()

		config, err := utils.InitConfig()
		if err != nil {
			logger.Fatal().Err(err).Msg("Could not read config")
			return err
		}

		efs_id := config.AWS.EFSId
		efs_mountpoint := config.AWS.EFSMountPoint

		// mount EFS locally
		out, err := exec.Command(
			"sudo", "mount",
			"-t", "efs",
			efs_id, efs_mountpoint,
		).Output()

		if err != nil {
			return err
		}

		logger.Info().Msgf("mounted volume successfully with output: %v", out)

		// Config storage here takes some finagling because we're dumping everything into the EFS storage.
		// For now, we should be OK with simply copying from local storage dir to the attached instance.

		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCommand)
	setupCommand.AddCommand(awsCommand)

	awsCommand.AddCommand(efsAttachCommand)
}
