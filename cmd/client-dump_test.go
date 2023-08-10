package cmd

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/nravic/cedana/utils"
	"google.golang.org/protobuf/proto"
)

func BenchmarkDump(b *testing.B) {
	c, err := instantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	for {
		// TODO BS Need to add time out here
		filename, err := LookForPid()
		if err != nil {
			b.Errorf("Error in LookForPid(): %v", err)
		}
		if filename != "" {
			// Open the file for reading
			file, err := os.Open("../benchmarking/pids/pid-loop.txt")
			if err != nil {
				fmt.Println("Error opening file:", err)
				return
			}
			defer file.Close()

			// Read the bytes from the file
			var pidBytes [8]byte // Assuming int64 is 8 bytes
			_, err = file.Read(pidBytes[:])
			if err != nil {
				fmt.Println("Error reading from file:", err)
				return
			}

			// Convert bytes to int64
			pidInt32 := int32(binary.LittleEndian.Uint64(pidBytes[:]))
			c.process.PID = pidInt32
			break
		}
	}

	// We want a list of all binaries that are to be ran and benchmarked,
	// have them write their pid to temp files on disk and then have the testing suite read from them

	for i := 0; i < b.N; i++ {
		err := c.dump("../benchmarking/temp/")
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}

}

func TestDump(t *testing.T) {
	cmd := exec.Command("/bin/sh", "../cmd/run_benchmarks.sh")
	err := cmd.Run()

	if err != nil {
		t.Errorf("Error in cmd.Run(): %v", err)
	}
}

func LookForPid() (string, error) {
	dirPath := "../benchmarking/pids/"

	// Open the directory
	dir, err := os.Open(dirPath)
	if err != nil {
		fmt.Println("Error opening directory:", err)
		return "", err
	}
	defer dir.Close()

	// Read the directory contents
	fileInfos, err := dir.Readdir(-1)
	if err != nil {
		fmt.Println("Error reading directory contents:", err)
		return "", err
	}

	// Iterate over the files
	for _, fileInfo := range fileInfos {
		if fileInfo.Mode().IsRegular() {
			return fileInfo.Name(), err
		}
	}
	err = fmt.Errorf("No files found in directory")
	return "", err
}

func PostDumpCleanup() {
	c, _ := instantiateClient()
	// Code to run after the benchmark
	data, err := os.ReadFile("../benchmarking/results/cpu.prof.gz")
	// len of data is 0 for some reason

	c.logger.Log().Msgf("data: %+v", string(data))

	if err != nil {
		c.logger.Error().Msgf("Error in os.ReadFile(): %v", err)
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(data))

	if err != nil {
		c.logger.Error().Msgf("Error in gzip.NewReader(): %v", err)
	}

	defer gzipReader.Close()

	decompressedData, err := io.ReadAll(gzipReader)

	if err != nil {
		c.logger.Error().Msgf("Error in ioutil.ReadAll(): %v", err)
	}

	c.logger.Log().Msgf("decompressed data: %+v", string(decompressedData))

	profile := utils.Profile{}

	proto.Unmarshal(decompressedData, &profile)

	c.logger.Log().Msgf("proto data duration: %+v", profile.DurationNanos)
}

func TestMain(m *testing.M) {
	// Code to run before the tests
	m.Run()
	// Code to run after the tests
	PostDumpCleanup()
}
