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
		table.SetHeader([]string{"Job ID", "PID", "Status", "Checkpoint"})

		resp, err := cts.Query(ctx, &task.QueryArgs{}) // get all processes
		if err != nil {
			log.Error().Err(err).Msgf("error querying processes")
			return err
		}

		if len(resp.Processes) == 0 {
			fmt.Println("No managed processes")
			return nil
		}

		for _, v := range resp.Processes {
			var checkpoint, status string
			if v.RemoteState != nil {
				// For now just grab latest checkpoint
				checkpoint = fmt.Sprintf("%s (remote)", v.RemoteState[len(v.RemoteState)-1].CheckpointID)
			} else {
				checkpoint = v.CheckpointPath
			}

			status = v.JobState.String()

			pid := strconv.Itoa(int(v.PID))

			table.Append([]string{v.JID, pid, status, checkpoint})
		}

		table.Render()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(psCmd)
}
