package cmd

import (
	"testing"
)

func BenchmarkRestore(b *testing.B) {
	for i := 0; i < b.N; i++ {
		err := c.restore("../benchmarking/temp/")
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}
}
