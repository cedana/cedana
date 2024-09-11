package cmd

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

var queueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Add things to internal job queue",
	Args:  cobra.ArbitraryArgs,
}

var queueCheckpointCmd = &cobra.Command{
	Use:   "checkpoint",
	Short: "Checkpoint",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		service, err := services.NewClient()
		if err != nil {
			log.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer service.Close()
		containerName, err := cmd.Flags().GetString(queueContainerNameFlag)
		if err != nil {
			log.Error().Msgf("Error fetching %s flag: %v", queueContainerNameFlag, err)
			return err
		}
		containerNs, err := cmd.Flags().GetString(queueNamespaceFlag)
		if err != nil {
			log.Error().Msgf("Error fetching %s flag: %v", queueNamespaceFlag, err)
			return err
		}
		containerImage, err := cmd.Flags().GetString(queueImageNameFlag)
		if err != nil {
			log.Error().Msgf("Error fetching %s flag: %v", queueImageNameFlag, err)
			return err
		}

		jobId := fmt.Sprintf("localjob-%d", rand.Uint64())
		service.QueueJobStatus(ctx, &task.QueueJobID{JobID: jobId})
		b, err := service.QueueCheckpoint(ctx, &task.QueueJobCheckpointRequest{
			ContainerName: containerName,
			Namespace:     containerNs,
			PodName:       "localjob",
			ImageName:     containerImage,
			Id:            jobId,
		})
		_ = b.Value
		return nil
	},
}

var queueWaitCmd = &cobra.Command{
	Use:   "wait",
	Short: "Wait",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("Atleast 1 jobId is required for wait")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 480*time.Second)
		defer cancel()
		jobIds := args
		service, err := services.NewClient()
		if err != nil {
			log.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer service.Close()

		status, err := cmd.Flags().GetString(queueStatusFlag)
		if err != nil {
			log.Error().Msgf("Error getting status: %v", err)
			return err
		}
		status = strings.ToLower(status)
		matchAgainstSuccess := status == "success" || (strings.HasPrefix(status, "suc") && strings.Contains(status, "e"))
		var success = false
		var wg sync.WaitGroup
		wg.Add(1)
		outSet := map[int]struct{}{}
		go func() {
			defer wg.Done()
			for {
				for idx, jobId := range jobIds {
					if _, f := outSet[idx]; f {
						continue
					}
					s, err := service.QueueJobStatus(ctx, &task.QueueJobID{JobID: jobId})
					if err != nil {
						log.Error().Err(err).Msg("Failed to fetch job status")
						return
					}
					if matchAgainstSuccess {
						if s.Status == task.QueueJobStatusEnum_StatusSuccess {
							outSet[idx] = struct{}{}
						} else if s.Status == task.QueueJobStatusEnum_StatusFail {
							return
						}
					} else {
						if s.Status == task.QueueJobStatusEnum_StatusFail {
							outSet[idx] = struct{}{}
						} else {
							return
						}
					}
				}
				if len(outSet) == len(jobIds) {
					success = true
					return
				}
				select {
				case <-ctx.Done():
					return
				default:
				}
			}
		}()
		wg.Wait()

		if !success {
			log.Error().Msg("Atleast a Job status returned failed")
			os.Exit(1)
		} else {
			log.Debug().Msg("All jobs successful")
		}

		return nil
	},
}

var queueContainerNameFlag = "name"
var queueNamespaceFlag = "ns"
var queueImageNameFlag = "image"

var queueStatusFlag = "status"

func init() {
	queueCheckpointCmd.Flags().String(queueContainerNameFlag, "", "Set the container id for the checkpoint being queued")
	queueCheckpointCmd.Flags().String(queueNamespaceFlag, "", "Set the container namespace for the checkpoint being queued")
	queueCheckpointCmd.Flags().String(queueImageNameFlag, "", "Set the output image name(including tag) for the checkpoint being queued")
	queueCmd.AddCommand(queueCheckpointCmd)
	queueWaitCmd.Flags().String(queueStatusFlag, "success", "Set the wait status type, default is success")
	queueCmd.AddCommand(queueWaitCmd)

	rootCmd.AddCommand(queueCmd)
}
