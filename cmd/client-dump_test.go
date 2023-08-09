package cmd

import (
	"bytes"
	"compress/gzip"
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

	c.process.PID = 659104
	// We want a list of all binaries that are to be ran and benchmarked,
	// have them write their pid to temp files on disk and then have the testing suite read from them

	for i := 0; i < b.N; i++ {
		err := c.dump(c.config.SharedStorage.DumpStorageDir)
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}

}

func TestDump(t *testing.T) {
	cmd := exec.Command("/bin/sh", "/home/brandonsmith738/cedana/cedana/cmd/run_benchmarks.sh")
	err := cmd.Run()

	if err != nil {
		t.Errorf("Error in cmd.Run(): %v", err)
	}
}

func PostDumpCleanup() {
	c, _ := instantiateClient()
	// Code to run after the benchmark
	data, err := os.ReadFile("/home/brandonsmith738/cedana/cedana/benchmarking/results/cpu.prof.gz")
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
