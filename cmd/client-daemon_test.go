package cmd

import (
	"syscall"
	"testing"
	"time"

	cedana "github.com/cedana/cedana/types"
	"github.com/cedana/cedana/utils"
)

func TestClient_StartJob(t *testing.T) {
	// Test case: Task is empty
	t.Run("TaskIsEmpty", func(t *testing.T) {
		c := &Client{
			config: &utils.Config{
				Client: utils.Client{
					Task: "",
				},
			},
		}

		_, err := c.runTask(c.config.Client.Task)

		// Verify that the error is returned
		if err == nil {
			t.Error("Expected error, but got nil")
		}
	})

	// Test case: Task is not empty
	t.Run("TaskIsNotEmpty", func(t *testing.T) {
		c := &Client{
			config: &utils.Config{
				Client: utils.Client{
					Task: "echo 'Hello, World!'; sleep 5",
				},
			},
		}

		pid, err := c.runTask(c.config.Client.Task)

		// Verify that no error is returned
		if err != nil {
			t.Errorf("Expected no error, but got %v", err)
		}

		// Verify that the pid is greater than 0
		if pid <= 0 {
			t.Errorf("Expected pid > 0, but got %d", pid)
		}

		// Verify that the process is actually detached
		if syscall.Getppid() != syscall.Getpgrp() {
			t.Error("Expected process to be detached")
		}
	})

	t.Run("TaskFailsOnce", func(t *testing.T) {
		c := &Client{
			config: &utils.Config{
				Client: utils.Client{
					Task: "",
				},
			},
			channels: &CommandChannels{
				recover_command: make(chan cedana.ServerCommand),
			},
		}

		go mockServerRetryCmd(c)
		err := c.tryStartJob()
		if err != nil {
			t.Errorf("Expected no error, but got %v", err)
		}
	})
}

func mockServerRetryCmd(c *Client) {
	// wait 30 seconds and fire a message on the recover channel
	// that breaks enterDoomLoop(), to update the runTask() for loop
	time.Sleep(30 * time.Second)
	c.channels.recover_command <- cedana.ServerCommand{
		UpdatedTask: "echo 'Hello, World!'; sleep 5",
	}
}
