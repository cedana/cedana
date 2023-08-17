package utils

import (
	"testing"
)

func BenchmarkZiping(b *testing.B) {
	dumpDir := "benchmarking/temp/pytorch/_usr_bin_python3_10_17_08_2023_1204"

	for i := 0; i < b.N; i++ {
		err := ZipFolder("pytorch.zip", dumpDir)
		if err != nil {
			b.Errorf("Error in ZipFolder(): %v, command: %v", err, "zip -r "+"pytorch.zip "+dumpDir)
		}
	}
}
