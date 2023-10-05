package api

import (
	"testing"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/utils"
	"github.com/spf13/afero"
)

func TestClient_WriteOnlyFds(t *testing.T) {
	openFds := []*task.OpenFilesStat{
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
		Logger: &logger,
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

// TODO BS: this will mock what cedana market orchestrator does instead
// func mockServerRetryCmd(c *Client) {
// 	// wait 30 seconds and fire a message on the recover channel
// 	// that breaks enterDoomLoop(), to update the runTask() for loop
// 	time.Sleep(10 * time.Second)
// 	c.channels.retryCmdBroadcaster.Broadcast(cedana.ServerCommand{
// 		UpdatedTask: "echo 'Hello, World!'",
// 	})
// }

func contains(paths []string, path string) bool {
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}
