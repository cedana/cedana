package cmd

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/client"
	"github.com/spf13/cobra"
)

const (
	DEFAULT_PROTOCOL    = "tcp"
	DEFAULT_ADDRESS     = "0.0.0.0:8080"
	MAX_RETRIES         = 5
	CLIENT_RETRY_PERIOD = time.Second
)

//go:embed scripts/setup-host.sh
var setupHostScript string

//go:embed scripts/cleanup-host.sh
var cleanupHostScript string

//go:embed scripts/bump-restart.sh
var restartScript string

//go:embed scripts/start-chroot.sh
var startChrootScript string

func init() {
	HelperCmd.AddCommand(destroyCmd)

	HelperCmd.Flags().Bool("setup-host", false, "Setup host for Cedana")
	HelperCmd.Flags().Bool("restart", false, "Restart the Cedana daemon on the host")
	HelperCmd.Flags().Bool("start-chroot", false, "Start chroot and Cedana daemon")
}

var HelperCmd = &cobra.Command{
	Use:   "k8s-helper",
	Short: "Helper for running in Kubernetes",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		restart, _ := cmd.Flags().GetBool("restart")
		if restart {
			if err := runScript(ctx, restartScript, true); err != nil {
				return fmt.Errorf("error restarting: %w", err)
			}
		}

		setupHost, _ := cmd.Flags().GetBool("setup-host")
		if setupHost {
			if err := runScript(ctx, setupHostScript, true); err != nil {
				return fmt.Errorf("error setting up host: %w", err)
			}
		}

		startChroot, _ := cmd.Flags().GetBool("start-chroot")
		startChroot = startChroot || setupHost

		return startHelper(ctx, startChroot, DEFAULT_ADDRESS, DEFAULT_PROTOCOL)
	},
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy cedana from host of kubernetes worker node",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if err := destroyCedana(ctx); err != nil {
			return fmt.Errorf("error destroying cedana on host: %w", err)
		}

		return nil
	},
}

func destroyCedana(ctx context.Context) error {
	if err := runScript(ctx, cleanupHostScript, true); err != nil {
		return fmt.Errorf("error cleaning up host: %w", err)
	}

	return nil
}

func startHelper(ctx context.Context, startChroot bool, address string, protocol string) error {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)
	var w sync.WaitGroup

	_, err := createClientWithRetry(address, protocol)
	if err != nil {
		return fmt.Errorf("failed to create client after %d attempts: %w", MAX_RETRIES, err)
	}

	// Goroutine to check if the daemon is running
	w.Add(1)
	go func() {
		defer w.Done()
		ticker := time.NewTicker(time.Second * 10)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				isRunning, err := isDaemonRunning(ctx, address, protocol)
				if err != nil {
					fmt.Printf("Error checking if daemon is running: %v\n", err)
					continue
				}
				if !isRunning {
					fmt.Printf("Daemon is not running. Restarting...\n")

					err := startDaemon(ctx, startChroot, address, protocol)
					if err != nil {
						fmt.Printf("Error restarting Cedana: %v\n", err)
						continue
					}

					_, err = createClientWithRetry(address, protocol)
					if err != nil {
						fmt.Printf("Failed to create client after %d attempts: %v\n", MAX_RETRIES, err)
						continue
					}

					fmt.Println("Daemon restarted.")
				}

			case <-signalChannel:
				fmt.Println("Received kill signal. Exiting...")
				os.Exit(0)
			}
		}
	}()

	// scrape daemon logs for kubectl logs output
	go func() {
		defer w.Done()
		file, err := os.Open("/host/var/log/cedana-daemon.log")
		if err != nil {
			fmt.Println("Failed to open cedana-daemon.log")
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
				fmt.Println("Error reading cedana-daemon.log")
				return
			}
			trimmed := strings.TrimSpace(line)
			if len(trimmed) > 0 {
				// we don't use the log function as the logs should have their own timing data
				fmt.Println(trimmed)
			}
		}
	}()

	w.Wait()

	return nil
}

func startDaemon(ctx context.Context, startChroot bool, address string, protocol string) error {
	if startChroot {
		err := runScript(ctx, startChrootScript, true)
		if err != nil {
			return err
		}
	} else {
		err := runCommand(ctx, "cedana", "daemon", "start", "--address", address, "--protocol", protocol)
		if err != nil {
			return err
		}
	}

	return nil
}

func createClientWithRetry(address, protocol string) (*client.Client, error) {
	var c *client.Client
	var err error

	for i := 0; i < MAX_RETRIES; i++ {
		c, err = client.New(address, protocol)
		if err == nil {
			// Successfully created the client, break out of the loop
			break
		}

		fmt.Printf("Error creating client: %v. Retrying...\n", err)
		time.Sleep(CLIENT_RETRY_PERIOD)

		if i == MAX_RETRIES-1 {
			// If it's the last attempt, return the error
			return nil, fmt.Errorf("failed to create client after %d attempts", MAX_RETRIES)
		}
	}

	return c, nil
}

func isDaemonRunning(ctx context.Context, protocol, address string) (bool, error) {
	client, err := client.New(address, protocol)
	if err != nil {
		return false, err
	}
	defer client.Close()
	return client.HealthCheckConnection(ctx)
}

func runCommand(ctx context.Context, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runScript(ctx context.Context, script string, logOutput bool) error {
	cmd := exec.CommandContext(ctx, "bash")
	cmd.Stdin = strings.NewReader(script)

	if logOutput {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}
