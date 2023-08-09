package cmd

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/nravic/cedana/utils"
	"google.golang.org/protobuf/proto"
)

func FindTotalMemoryUsage(filepath string) string {
	content, _ := os.ReadFile(filepath)

	// Convert the content to a string
	input := string(content)

	// Define a regular expression pattern to match the desired value
	pattern := `([\d.]+)kB total`

	// Compile the regular expression
	re := regexp.MustCompile(pattern)

	// Find the first match in the input string
	match := re.FindStringSubmatch(input)

	// Check if there's a match
	if len(match) > 1 {
		value := match[1]
		fmt.Println("Extracted value:", value)
		return value
	} else {
		fmt.Println("No match found.")
		return ""
	}
}

func BenchmarkDump(b *testing.B) {
	data, _ := os.ReadFile("/home/brandonsmith738/cedana/cedana/benchmarking/results/cpu.prof")
	profile := utils.Profile{}

	proto.Unmarshal(data, &profile)

	c, err := instantiateClient()

	c.logger.Log().Msgf("proto data: %+v", profile.DurationNanos)

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	value := FindTotalMemoryUsage("benchmarking/results/mem_profile.txt")

	fmt.Print(value)
	err = os.WriteFile("benchmarking/results/out.txt", []byte(value), 0644)

	if err != nil {
		b.Errorf("Error in os.WriteFile(): %v", err)
	}

	c.process.PID = 602376
	// We want a list of all binaries that are to be ran and benchmarked,
	// have them write their pid to temp files on disk and then have the testing suite read from them

	for i := 0; i < b.N; i++ {
		err := c.dump(c.config.SharedStorage.DumpStorageDir)
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}
}
