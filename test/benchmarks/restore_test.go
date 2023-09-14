package test

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/cedana/cedana/cmd"
)

func BenchmarkLoopRestore(b *testing.B) {
	skipCI(b)
	c, err := cmd.InstantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	checkpoint, isError := setup(b, "benchmarking/temp/loop/")
	if isError {
		return
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := c.Restore(nil, &checkpoint)
		if err != nil {
			b.Errorf("Error in c.restore(): %v", err)
		}
		b.StopTimer()
		destroyPid(b, c)
		b.StartTimer()
	}
	b.Cleanup(func() {
		finishBenchmark(b, c)
		FileIPCCleanup(b, "benchmarking/temp/loop/", "restore")
	})

}

func BenchmarkServerRestore(b *testing.B) {
	skipCI(b)
	c, err := cmd.InstantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	checkpoint, isError := setup(b, "benchmarking/temp/server/")
	if isError {
		return
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := c.Restore(nil, &checkpoint)
		if err != nil {
			b.Errorf("Error in c.restore(): %v", err)
		}
		b.StopTimer()
		destroyPid(b, c)
		b.StartTimer()
	}
	b.Cleanup(func() {
		finishBenchmark(b, c)
		FileIPCCleanup(b, "benchmarking/temp/server/", "restore")
	})

}

func BenchmarkPytorchRestore(b *testing.B) {
	skipCI(b)
	c, err := cmd.InstantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	checkpoint, isError := setup(b, "benchmarking/temp/pytorch/")
	if isError {
		return
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := c.Restore(nil, &checkpoint)
		if err != nil {
			b.Errorf("Error in c.restore(): %v", err)
		}
		b.StopTimer()
		destroyPid(b, c)
		b.StartTimer()
	}
	b.Cleanup(func() {
		finishBenchmark(b, c)
		FileIPCCleanup(b, "benchmarking/temp/pytorch/", "restore")

	})

}

func BenchmarkRegressionRestore(b *testing.B) {
	skipCI(b)
	c, err := cmd.InstantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	checkpoint, isError := setup(b, "benchmarking/temp/pytorch-regression/")
	if isError {
		return
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := c.Restore(nil, &checkpoint)
		if err != nil {
			b.Errorf("Error in c.restore(): %v", err)
		}
		b.StopTimer()
		destroyPid(b, c)
		b.StartTimer()
	}
	b.Cleanup(func() {
		finishBenchmark(b, c)
		FileIPCCleanup(b, "benchmarking/temp/pytorch-regression/", "restore")
	})

}
func BenchmarkVisionRestore(b *testing.B) {
	skipCI(b)
	c, err := cmd.InstantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	checkpoint, isError := setup(b, "benchmarking/temp/pytorch-vision/")
	if isError {
		return
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := c.Restore(nil, &checkpoint)
		if err != nil {
			b.Errorf("Error in c.restore(): %v", err)
		}
		b.StopTimer()
		destroyPid(b, c)
		b.StartTimer()
	}
	b.Cleanup(func() {
		finishBenchmark(b, c)
		FileIPCCleanup(b, "benchmarking/temp/pytorch-vision/", "restore")
	})

}

func finishBenchmark(b *testing.B, c *cmd.Client) {
	_, err := os.Stat("cedana_restore")
	if err == nil {
		os.RemoveAll("cedana_restore")
	}
	_, err = os.Stat("../../output.log")
	if err == nil {
		os.Remove("../../output.log")
	}

	destroyPid(b, c)

	err = os.WriteFile("benchmarking/temp/type", []byte("restore"), 0o644)

	if err != nil {
		b.Errorf("Error in os.WriteFile(): %v", err)
	}
}

func setup(b *testing.B, dir string) (string, bool) {
	files, err := os.ReadDir(dir)
	var filename string

	if err != nil {
		b.Errorf("Error in os.ReadDir(): %v", err)
		return "", true
	}

	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".zip") {
			filename = file.Name()
			break
		}
	}
	if filename == "" {
		b.Errorf("No .zip files found in directory: %v", dir)
		return "", true
	}

	checkpoint := dir + filename

	_, err = os.Stat(checkpoint)

	if err != nil {
		b.Errorf("Error in os.Stat(): %v", err)
	}
	return checkpoint, false
}

func destroyPid(b *testing.B, c *cmd.Client) {
	pids, err := getFilenames("benchmarking/pids/", "")

	if err != nil {
		b.Errorf("Error in getFilenames(): %v", err)
	}

	_, pid, _ := LookForPid(c, pids)

	if len(pid) == 0 {
		return
	}

	for _, pid := range pid {
		process, err := os.FindProcess(int(pid))
		if err != nil {
			fmt.Println("Error finding process:", err)
		}

		err = process.Signal(syscall.SIGKILL)
		if err != nil {
			fmt.Println("Error sending signal:", err)
		}
	}
}
