package cmd

// This file contains all the ps-related commands when starting `cedana ps ...`

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	bolt "go.etcd.io/bbolt"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List managed processes",
	Run: func(cmd *cobra.Command, args []string) {
		cts, err := services.NewClient(api.Address)
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"Job ID", "PID", "Status", "Local Checkpoint Path", "Remote Checkpoint ID"})

		if _, err := os.Stat(api.DBPath); err == nil {
			// open db in read-only mode
			conn, err := bolt.Open(api.DBPath, 0600, &bolt.Options{ReadOnly: true})
			if err != nil {
				logger.Fatal().Err(err).Msg("Could not open or create db")
				return
			}

			defer conn.Close()

			// job ID, PID, isRunning, CheckpointPath, Remote checkpoint ID
			var data [][]string
			err = conn.View(func(tx *bolt.Tx) error {
				root := tx.Bucket([]byte("default"))
				if root == nil {
					return fmt.Errorf("could not find bucket")
				}

				return root.ForEachBucket(func(k []byte) error {
					job := root.Bucket(k)
					jobId := string(k)
					return job.ForEach(func(k, v []byte) error {
						var state task.ProcessState
						var remoteCheckpointID string
						var status string
						err := json.Unmarshal(v, &state)
						if err != nil {
							return err
						}

						if state.RemoteState != nil {
							// For now just grab latest checkpoint
							remoteCheckpointID = state.RemoteState[len(state.RemoteState)-1].CheckpointID
						}

						if state.ProcessInfo != nil {
							status = state.ProcessInfo.Status
						}

						data = append(data, []string{jobId, string(k), status, state.CheckpointPath, remoteCheckpointID})
						return nil
					})
				})
			})
			if err != nil {
				return
			}

			for _, v := range data {
				table.Append(v)
			}
		}

		table.Render() // Send output
	},
}

func init() {
	rootCmd.AddCommand(psCmd)
}
