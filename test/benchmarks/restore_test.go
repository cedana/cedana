package test

import (
	"os"
	"strings"
	"testing"

	"github.com/cedana/cedana/cmd"
)

func BenchmarkRestore(b *testing.B) {
	skipCI(b)
	c, err := cmd.InstantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	// TODO BS
	// Here need to loop through all the files in the directory and find first zip dir.
	// There really should only be two directories at all times
	dir := "../benchmarking/temp/"

	// List all files in the directory
	files, err := os.ReadDir(dir)
	var filename string

	if err != nil {
		b.Errorf("Error in os.ReadDir(): %v", err)
		return
	}

	// Loop through the files and find the first .zip file
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".zip") {
			filename = file.Name()
			break
		}
	}
	if filename == "" {
		b.Errorf("No .zip files found in directory: %v", dir)
		return
	}

	checkpoint := dir + filename

	if err != nil {
		b.Errorf("Error in os.Stat(): %v", err)
	}

	for i := 0; i < b.N; i++ {
		err := c.Restore(nil, &checkpoint)
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}
}
