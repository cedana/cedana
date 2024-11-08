package cmd

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var attachCmd = &cobra.Command{
	Use:   "attach <PID>",
	Short: "Attach stdin/out/err to a process/container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		port := viper.GetUint32("options.port")
		host := viper.GetString("options.host")

		client, err := NewClient(host, port)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}

		pid, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("PID must be an integer")
		}

		stream, err := client.Attach(cmd.Context(), &daemon.AttachReq{PID: uint32(pid)})
		if err != nil {
			return err
		}
		stdIn, stdOut, stdErr, exitCode, errors := utils.NewStreamIOMaster(stream)

		go io.Copy(stdIn, os.Stdin) // since stdin never closes
		outDone := utils.CopyNotify(os.Stdout, stdOut)
		errDone := utils.CopyNotify(os.Stderr, stdErr)
		<-outDone // wait to capture all out
		<-errDone // wait to capture all err

		if err := <-errors; err != nil {
			return GRPCError(err)
		}

		os.Exit(<-exitCode)

		return nil
	},
}
