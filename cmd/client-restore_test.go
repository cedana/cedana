package cmd

import (
	"testing"
)

func BenchmarkRestore(b *testing.B) {
	c, err := instantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	// TODO BS
	// Here need to loop through all the files in the directory and find first zip dir.
	// There really should only be two directories at all times
	checkpoint := "../benchmarking/temp/"

	if err != nil {
		b.Errorf("Error in os.Stat(): %v", err)
	}

	for i := 0; i < b.N; i++ {
		err := c.restore(nil, &checkpoint)
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}
}
