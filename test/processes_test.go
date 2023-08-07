package test

import (
	"fmt"
	"testing"

	"github.com/nravic/cedana/utils"
	ps "github.com/shirou/gopsutil/v3/process"
)

func TestGetProcessSimilarity(t *testing.T) {
	// set alternative HOST_PROC for testing
	// wondering how useful it might be to use afero here instead, TODO NR
	t.Setenv("HOST_PROC", "testdata/proc")
	processes, err := ps.Processes()
	fmt.Printf("Processes: %v\n", processes)
	if err != nil {
		t.Errorf("Error: %v", err)
	}

	t.Run("python process", func(t *testing.T) {
		var expectedPid int32
		processName := "jupyter notebook &"
		expectedPid = 1266999

		pid, err := utils.GetProcessSimilarity(processName, processes)
		if err != nil {
			t.Errorf("Error: %v", err)
		}
		if pid != expectedPid {
			t.Errorf("Expected PID: %d, got: %d", expectedPid, pid)
		}
	})

	t.Run("server match", func(t *testing.T) {
		var expectedPid int32
		processName := "./server -m models/7B/ggml-model-q4_0.bin -c 2048 &"
		expectedPid = 1227709

		pid, err := utils.GetProcessSimilarity(processName, processes)
		if err != nil {
			t.Errorf("Error: %v", err)
		}
		if pid != expectedPid {
			t.Errorf("Expected PID: %d, got: %d", expectedPid, pid)
		}
	})

}
