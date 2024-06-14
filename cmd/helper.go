package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	maxRetries        = 5
	clientRetryPeriod = time.Second
)

var helperCmd = &cobra.Command{
	Use:   "k8s-helper",
	Short: "Helper for Cedana running in Kubernetes",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)

		setupHost, _ := cmd.Flags().GetBool("setup-host")
		if setupHost {
			if err := runCommand("bash", "./scripts/k8s/setup-host.sh"); err != nil {
				logger.Error().Err(err).Msg("Error setting up host")
			}
		}

		startChroot, _ := cmd.Flags().GetBool("start-chroot")
		if startChroot {
			if err := runCommand("bash", "./scripts/k8s/chroot-start.sh"); err != nil {
				logger.Error().Err(err).Msg("Error with chroot and starting daemon")
			}
		}
		startHelper(ctx, startChroot)

		return nil
	},
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy cedana from host of kubernetes worker node",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)

		if err := destroyCedana(ctx); err != nil {
			logger.Error().Err(err).Msg("Unable to destroy cedana on host.")
		}

		return nil
	},
}

func destroyCedana(ctx context.Context) error {
	logger := ctx.Value("logger").(*zerolog.Logger)

	return nil
}

func startHelper(ctx context.Context, startChroot bool) {
	logger := ctx.Value("logger").(*zerolog.Logger)
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)

	cts, err := createClientWithRetry()
	if err != nil {
		log.Fatalf("Failed to create client after %d attempts: %v", maxRetries, err)
	}

	// Goroutine to check if the daemon is running
	go func() {
		ticker := time.NewTicker(time.Second * 10)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				isRunning, err := isProcessRunning()
				if err != nil {
					logger.Error().Err(err).Msg("Issue checking if daemon is running")
				}
				if !isRunning {
					logger.Info().Msg("Daemon is not running. Restarting...")

					err := startDaemon(startChroot)
					if err != nil {
						logger.Error().Err(err).Msg("Error restarting Cedana")
					}

					cts, err = createClientWithRetry()
					if err != nil {
						log.Fatalf("Failed to create client after %d attempts: %v", maxRetries, err)
					}

					log.Println("Daemon restarted.")
				}

			case <-signalChannel:
				fmt.Println("Received kill signal. Exiting...")
				os.Exit(0)
			}
		}
	}()
	req := &task.ContainerdQueryArgs{}
	cts.ContainerdQuery(context.Background(), req)

	select {}
}

func createClientWithRetry() (*services.ServiceClient, error) {
	var client *services.ServiceClient
	var err error

	for i := 0; i < maxRetries; i++ {
		client, err = services.NewClient()
		if err == nil {
			// Successfully created the client, break out of the loop
			break
		}

		log.Printf("Error creating client: %v. Retrying...", err)
		time.Sleep(clientRetryPeriod)

		if i == maxRetries-1 {
			// If it's the last attempt, return the error
			return nil, fmt.Errorf("Failed to create client after %d attempts", maxRetries)
		}
	}

	return client, nil
}

func runCommand(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func startDaemon(startChroot bool) error {
	if startChroot {
		err := runCommand("bash", "./scripts/k8s/chroot-start.sh")
		if err != nil {
			return err
		}

	} else {
		err := runCommand("bash", "-c", "cedana daemon start")
		if err != nil {
			return err
		}
	}

	return nil
}

func isProcessRunning() (bool, error) {
	conn, err := grpc.Dial(api.ADDRESS, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false, err
	}

	conn.Close()
	return true, nil
}

func init() {
	helperCmd.Flags().Bool("setup-host", false, "Setup host for Cedana")
	helperCmd.Flags().Bool("start-chroot", false, "Start chroot and Cedana daemon")
	rootCmd.AddCommand(helperCmd)

	helperCmd.AddCommand(destroyCmd)
}
