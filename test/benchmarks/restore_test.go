package test

import (
	"os"
	"path/filepath"
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
	dir := "../benchmarking/temp/loop/"

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

	_, err = os.Stat(checkpoint)

	if err != nil {
		b.Errorf("Error in os.Stat(): %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := c.Restore(nil, &checkpoint)
		if err != nil {
			b.Errorf("Error in c.restore(): %v", err)
		}
	}
	b.Cleanup(func() {
		_, err := os.Stat("cedana_restore")
		if err == nil {
			os.RemoveAll("cedana_restore")
		}
		_, err = os.Stat("../output.log")
		if err == nil {
			os.Remove("../output.log")
		}

		// List all pids
		files, err := os.ReadDir("../benchmarking/pids/")
		if err != nil {
			b.Error("Error reading directory:", err)
			return
		}

		// Loop through the pids and remove them
		for _, file := range files {
			filePath := filepath.Join("../benchmarking/pids/", file.Name())
			err := os.Remove(filePath)
			if err != nil {
				b.Errorf("Error removing file %s: %v\n", filePath, err)
			} else {
				b.Errorf("Removed file: %s\n", filePath)
			}
		}
	})

}
