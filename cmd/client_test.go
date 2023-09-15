package cmd

import (
	"os"
	"syscall"
	"testing"
	"time"

	cedana "github.com/cedana/cedana/types"
	"github.com/cedana/cedana/utils"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/spf13/afero"
)

func TestClient_WriteOnlyFds(t *testing.T) {
	openFds := []process.OpenFilesStat{
		{Fd: 1, Path: "/path/to/file1"},
		{Fd: 2, Path: "/path/to/file2 (deleted)"},
		{Fd: 3, Path: "/path/to/file3"},
	}

	fs := afero.NewMemMapFs()
	contents := map[string]string{
		"/proc/1/fdinfo/1": "flags: 010002",
		"/proc/1/fdinfo/2": "flags: 0100000", //readonly - should not pass
		"/proc/1/fdinfo/3": "flags: 0100004", // readonly & append - should not pass
	}

	mockFS := &afero.Afero{Fs: fs}
	mockFS.MkdirAll("/proc/1/fdinfo", 0755)

	for k, v := range contents {
		mockFS.WriteFile(k, []byte(v), 0644)
	}

	logger := utils.GetLogger()
	c := &Client{
		fs:     mockFS,
		logger: &logger,
	}

	paths := c.WriteOnlyFds(openFds, 1)

	// Test case 1: Check if the path of the first file is included in the output
	if !contains(paths, "/path/to/file1") {
		t.Errorf("expected path '/path/to/file1' to be included in the output, but it was not")
	}

	// Test case 2: Check if the path of the second file (with '(deleted)' suffix removed) is included in the output
	if contains(paths, "/path/to/file2") {
		t.Errorf("expected path '/path/to/file2' to not be included in the output, but it was")
	}

	// Test case 3: Check if the path of the third file is included in the output
	if contains(paths, "/path/to/file3") {
		t.Errorf("expected path '/path/to/file3' to not be included in the output, but it was")
	}
}

func TestClient_RunTask(t *testing.T) {
	// Test case: Task is empty
	t.Run("TaskIsEmpty", func(t *testing.T) {
		c := &Client{
			config: &utils.Config{
				Client: utils.Client{
					Task: "",
				},
			},
		}

		_, err := c.RunTask(c.config.Client.Task)

		// Verify that the error is returned
		if err == nil {
			t.Error("Expected error, but got nil")
		}
	})

	// Test case: Task is not empty
	t.Run("TaskIsNotEmpty", func(t *testing.T) {
		// skip this test for CI - the check for detached process fails
		// inside a docker container

		if os.Getenv("CI") != "" {
			t.Skip("Skipping test in CI environment")
		}

		c := &Client{
			config: &utils.Config{
				Client: utils.Client{
					Task: "echo 'Hello, World!'; sleep 5",
				},
			},
		}

		pid, err := c.RunTask(c.config.Client.Task)

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

}

func TestClient_TryStartJob(t *testing.T) {
	t.Run("TaskFailsOnce", func(t *testing.T) {
		logger := utils.GetLogger()

		// start a server
		utils.RunDefaultServer(t)

		js := utils.CreateTestJetstream(t)

		c := &Client{
			config: &utils.Config{
				Client: utils.Client{
					Task: "",
				},
			},
			channels: &CommandChannels{
				retryCmdBroadcaster: Broadcaster[cedana.ServerCommand]{},
			},
			logger: &logger,
			// enterDoomLoop() makes a JetStream call
			js: js,
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
	time.Sleep(10 * time.Second)
	c.channels.retryCmdBroadcaster.Broadcast(cedana.ServerCommand{
		UpdatedTask: "echo 'Hello, World!'",
	})
}

func contains(paths []string, path string) bool {
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}
