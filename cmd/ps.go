package cmd

// This file contains all the ps-related commands when starting `cedana ps ...`

import (
	"fmt"
	"os"
	"strconv"

	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/olekukonko/tablewriter"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List managed processes",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			log.Error().Err(err).Msg("error creating client")
			return err
		}
		defer cts.Close()

		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"Job ID", "Type", "PID", "Status", "Checkpoint", "GPU?"})

		resp, err := cts.JobQuery(ctx, &task.JobQueryArgs{}) // get all processes
		if err != nil {
			log.Error().Err(err).Msgf("error querying processes")
			return err
		}

		if len(resp.Processes) == 0 {
			fmt.Println("No managed processes")
			return nil
		}

		for _, v := range resp.Processes {
			status := v.JobState.String()
			pid := strconv.Itoa(int(v.PID))
			gpu := strconv.FormatBool(v.GPU)
			var typ string
			if v.ContainerID == "" {
				typ = "process"
			} else {
				typ = "runc"
			}

			table.Append([]string{v.JID, typ, pid, status, v.CheckpointPath, gpu})
		}

		table.Render()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(psCmd)
}
