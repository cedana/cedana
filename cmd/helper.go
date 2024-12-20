package cmd

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/api/services"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const (
	maxRetries        = 5
	clientRetryPeriod = time.Second
)

//go:embed scripts/k8s/setup-host.sh
var setupHostScript string

//go:embed scripts/k8s/cleanup-host.sh
var cleanupHostScript string

//go:embed scripts/k8s/bump-restart.sh
var restartScript string

//go:embed scripts/k8s/start-otelcol.sh
var startOtelColScript string

var helperCmd = &cobra.Command{
	Use:   "k8s-helper",
	Short: "Helper for Cedana running in Kubernetes",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		restart, _ := cmd.Flags().GetBool("restart")
		if restart {
			if err := runScript("bash", restartScript, true); err != nil {
				log.Error().Err(err).Msg("Error restarting")
			}
		}

		startOtelCol, _ := cmd.Flags().GetBool("start-otelcol")
		if startOtelCol {
			// check for signoz_access_token
			apikey, err := getTelemetryAPIKey()
			if err != nil {
				log.Error().Err(err).Msg("Error getting telemetry API key")
			}

			os.Setenv("SIGNOZ_ACCESS_TOKEN", apikey)
			os.Setenv("CEDANA_OTEL_ENABLED", "true")
			if err := runScript("bash", startOtelColScript, false); err != nil {
				log.Error().Err(err).Msg("Error starting otelcol")
			}
		}

		setupHost, _ := cmd.Flags().GetBool("setup-host")
		if setupHost {
			if err := runScript("bash", setupHostScript, true); err != nil {
				log.Error().Err(err).Msg("Error setting up host")
			}
		}

		port, _ := cmd.Flags().GetUint32(portFlag)
		startChroot, _ := cmd.Flags().GetBool("start-chroot")
        startChroot = startChroot || setupHost
		startHelper(ctx, startChroot, port)

		return nil
	},
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy cedana from host of kubernetes worker node",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if err := destroyCedana(ctx); err != nil {
			log.Error().Err(err).Msg("Unable to destroy cedana on host.")
		}

		return nil
	},
}

func destroyCedana(ctx context.Context) error {
	if err := runScript("bash", cleanupHostScript, true); err != nil {
		log.Error().Err(err).Msg("Cleanup host script failed")

		return err
	}

	return nil
}

func getTelemetryAPIKey() (string, error) {
	var apiKey string
	apiKey, ok := os.LookupEnv("SIGNOZ_ACCESS_TOKEN")
	if !ok {
		// try downloading from checkpointsvc
		cedana_auth_token, ok := os.LookupEnv("CEDANA_AUTH_TOKEN")
		if !ok {
			return "", fmt.Errorf("tried downloading API key from checkpointsvc but CEDANA_AUTH_TOKEN not set")
		}

		cedana_url, ok := os.LookupEnv("CEDANA_URL")
		if !ok {
			return "", fmt.Errorf("tried downloading API key from checkpointsvc but CEDANA_URL not set")
		}

		url := fmt.Sprintf("%s/k8s/apikey/signoz", cedana_url)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return "", fmt.Errorf("error creating request: %v", err)
		}

		req.Header.Set("Authorization", "Bearer "+cedana_auth_token)
		client := &http.Client{}

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("error making request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("error getting api key: %d", resp.StatusCode)
		}

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("error reading response body: %v", err)
		}

		apiKey = string(respBody)
	}

	return apiKey, nil
}

func startHelper(ctx context.Context, startChroot bool, port uint32) {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)

	_, err := createClientWithRetry(port)
	if err != nil {
		log.Fatal().Msgf("Failed to create client after %d attempts: %v", maxRetries, err)
	}

	// Goroutine to check if the daemon is running
	go func() {
		ticker := time.NewTicker(time.Second * 10)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				isRunning, err := isCedanaDaemonRunning(ctx, port)
				if err != nil {
					log.Error().Err(err).Msg("Issue checking if daemon is running")
				}
				if !isRunning {
					log.Info().Msg("Daemon is not running. Restarting...")

					err := startDaemon(startChroot)
					if err != nil {
						log.Error().Err(err).Msg("Error restarting Cedana")
					}

					_, err = createClientWithRetry(port)
					if err != nil {
						log.Fatal().Msgf("Failed to create client after %d attempts: %v", maxRetries, err)
					}

					log.Info().Msg("Daemon restarted.")
				}

			case <-signalChannel:
				log.Info().Msg("Received kill signal. Exiting...")
				os.Exit(0)
			}
		}
	}()

	// scrape daemon logs for kubectl logs output
	go func() {
		file, err := os.Open("/host/var/log/cedana-daemon.log")
		if err != nil {
			log.Error().Err(err).Msg("Failed to open cedana-daemon.log")
			return
		}
		defer file.Close()

		reader := bufio.NewReader(file)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					time.Sleep(1 * time.Second)
					continue
				}
				log.Error().Err(err).Msg("Error reading cedana-daemon.log")
				return
			}
			trimmed := strings.TrimSpace(line)
			if len(trimmed) > 0 {
				// we don't use the log function as the logs should have their own timing data
				fmt.Println(trimmed)
			}
		}
	}()

	select {}
}

func createClientWithRetry(port uint32) (*services.ServiceClient, error) {
	var client *services.ServiceClient
	var err error

	for i := 0; i < maxRetries; i++ {
		client, err = services.NewClient(port)
		if err == nil {
			// Successfully created the client, break out of the loop
			break
		}

		log.Printf("Error creating client: %v. Retrying...", err)
		time.Sleep(clientRetryPeriod)

		if i == maxRetries-1 {
			// If it's the last attempt, return the error
			return nil, fmt.Errorf("failed to create client after %d attempts", maxRetries)
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

func runScript(command, script string, logOutput bool) error {
	cmd := exec.Command(command)
	cmd.Stdin = strings.NewReader(script)

	if logOutput {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}

	return cmd.Run()
}

var startChrootScript = `
#!/bin/bash
chroot /host bash /cedana/scripts/run-cedana.sh
`

func startDaemon(startChroot bool) error {
	if startChroot {
		err := runScript("bash", startChrootScript, true)
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

func isCedanaDaemonRunning(ctx context.Context, port uint32) (bool, error) {
	conn, err := services.NewClient(port)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	return conn.HealthCheck(ctx)
}

func init() {
	helperCmd.Flags().Bool("setup-host", false, "Setup host for Cedana")
	helperCmd.Flags().Bool("restart", false, "Restart the cedana service on the host")
  helperCmd.Flags().Bool("start-chroot", false, "Start chroot and Cedana daemon")
	helperCmd.Flags().Bool("start-otelcol", false, "Start otelcol on the host")
	rootCmd.AddCommand(helperCmd)

	helperCmd.AddCommand(destroyCmd)
}
