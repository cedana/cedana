package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
)

const (
	maxRetries        = 5
	clientRetryPeriod = time.Second
	daemonCommand     = "./cedana/cedana"
)

func main() {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)

	daemonPid, err := initialize()
	if err != nil {
		log.Fatalf("Cedana init failed: %v", err)
	}

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
				isRunning, err := isProcessRunning(daemonPid)
				if err != nil {
					log.Printf("Issue checking if daemon is running")
				}
				if !isRunning {
					log.Println("Daemon is not running. Restarting...")

					daemonPid, err = startDaemon()
					if err != nil {
						log.Printf("Error restarting Cedana: %v", err)
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
	req := &task.CtrByNameArgs{}
	cts.GetRuncIdByName(req)

	select {}
}

func createClientWithRetry() (*services.ServiceClient, error) {
	var client *services.ServiceClient
	var err error

	for i := 0; i < maxRetries; i++ {
		client, err = services.NewClient("localhost:8080")
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

func initialize() (int, error) {

	if err := runCommand("bash", "./install.sh"); err != nil {
		return -1, err
	}

	pid, err := startDaemon()
	if err != nil {
		return -1, err
	}

	return pid, nil
}

func copyScript(src, dest string) error {
	cmd := exec.Command("cp", src, dest)
	return cmd.Run()
}

func runCommand(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func startDaemon() (int, error) {
	cmd := exec.Command("./start-daemon.sh")
	err := cmd.Start()
	if err != nil {
		return -1, err
	}

	pid := cmd.Process.Pid
	fmt.Printf("Started process with PID: %d\n", pid)

	isRunning, err := isProcessRunning(pid)
	if err != nil || !isRunning {
		return -1, err
	}

	return pid, nil
}

func isProcessRunning(pid int) (bool, error) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}

	// Signal 0 checks if process is running
	err = process.Signal(syscall.Signal(0))
	return err == nil, nil
}
