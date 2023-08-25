package cmd

import (
	"os"
	"strings"
	"testing"
)

func BenchmarkRestore(b *testing.B) {
	skipCI(b)
	c, err := instantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	// TODO BS
	// Here need to loop through all the files in the directory and find first zip dir.
	// There really should only be two directories at all times
	dir := "../benchmarking/temp/loop/"

	// List all files in the directory
	files, err := os.ReadDir(dir)
	var filename string

	if err != nil {
		c.logger.Error().Msgf("Error reading directory: %v", err)
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
		c.logger.Error().Msgf("No .zip files found in directory: %v", dir)
		return
	}

	checkpoint := dir + filename

	if err != nil {
		b.Errorf("Error in os.Stat(): %v", err)
	}

	for i := 0; i < b.N; i++ {
		err := c.restore(nil, &checkpoint)
		if err != nil {
			b.Errorf("Error in c.restore(): %v", err)
		}
	}
}
