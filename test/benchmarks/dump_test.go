package test

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/cedana/cedana/cmd"
	"github.com/cedana/cedana/utils"
	"github.com/glebarez/sqlite"
	"github.com/rs/xid"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

type DB struct {
	orm *gorm.DB
}

type Benchmarks []*Benchmark

type Benchmark struct {
	gorm.Model
	ID                 string `gorm:"primaryKey"`
	ProcessName        string
	TimeToCompleteInNS int64
	TotalMemoryUsed    int64
	ElapsedTimeMs      int64
	FileSize           int64
	CmdType            string
}

// we are skipping ci for now as we are using dump which requires criu, need to build criu on gh action
func skipCI(b *testing.B) {
	if os.Getenv("CI") != "" {
		b.Skip("Skipping testing in CI environment")
	}
}

func getFilenames(directoryPath string, prefix string) ([]string, error) {
	// Read the directory contents
	files, err := os.ReadDir(directoryPath)
	if err != nil {
		return nil, err
	}

	loopFilenames := []string{}

	// Iterate through the files and append filenames that match the prefix "loop-"
	for _, file := range files {
		if strings.HasPrefix(file.Name(), prefix) {
			loopFilenames = append(loopFilenames, file.Name())
		}
	}

	return loopFilenames, nil
}

func BenchmarkDumpLoop(b *testing.B) {
	skipCI(b)
	dumpDir := "../../benchmarking/temp/loop"
	c, err := cmd.InstantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	fileNames, err := getFilenames("../../benchmarking/pids/", "loop-")

	if err != nil {
		b.Errorf("Error in getFilenames(): %v", err)
	}

	_, pid, _ := LookForPid(c, fileNames)

	c.Process.PID = pid[0]

	// We want a list of all binaries that are to be ran and benchmarked,
	// have them write their pid to temp files on disk and then have the testing suite read from them

	for i := 0; i < b.N; i++ {
		err := c.Dump(dumpDir)
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}

	b.Cleanup(
		func() {
			FileIPCCleanup(b, dumpDir, "dump")
		},
	)
}

func BenchmarkDumpServer(b *testing.B) {
	skipCI(b)
	dumpDir := "../../benchmarking/temp/server"
	c, err := cmd.InstantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	_, pid, _ := LookForPid(c, []string{"server.pid"})

	// this will always be one pid
	// never no pids since the error above accounts for that
	c.Process.PID = pid[0]

	// We want a list of all binaries that are to be ran and benchmarked,
	// have them write their pid to temp files on disk and then have the testing suite read from them

	for i := 0; i < b.N; i++ {
		err := c.Dump(dumpDir)
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}

	b.Cleanup(
		func() {
			FileIPCCleanup(b, dumpDir, "dump")
		},
	)
}

func BenchmarkDumpPytorch(b *testing.B) {
	skipCI(b)
	dumpDir := "../../benchmarking/temp/pytorch"
	c, err := cmd.InstantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	_, pid, _ := LookForPid(c, []string{"pytorch.pid"})

	// this will always be one pid
	// never no pids since the error above accounts for that
	b.Logf("pid: %v", pid)
	c.Process.PID = pid[0]

	// We want a list of all binaries that are to be ran and benchmarked,
	// have them write their pid to temp files on disk and then have the testing suite read from them

	for i := 0; i < b.N; i++ {
		err := c.Dump(dumpDir)
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}

	b.Cleanup(
		func() {
			FileIPCCleanup(b, dumpDir, "dump")
		},
	)
}

func BenchmarkDumpPytorchVision(b *testing.B) {
	skipCI(b)
	dumpDir := "../../benchmarking/temp/pytorch-vision"
	c, err := cmd.InstantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	_, pid, _ := LookForPid(c, []string{"pytorch_vision.pid"})

	// this will always be one pid
	// never no pids since the error above accounts for that
	b.Logf("pid: %v", pid)
	c.Process.PID = pid[0]

	// We want a list of all binaries that are to be ran and benchmarked,
	// have them write their pid to temp files on disk and then have the testing suite read from them

	for i := 0; i < b.N; i++ {
		err := c.Dump(dumpDir)
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}

	b.Cleanup(
		func() {
			FileIPCCleanup(b, dumpDir, "dump")
		},
	)
}

func BenchmarkDumpPytorchRegression(b *testing.B) {
	skipCI(b)

	dumpDir := "../../benchmarking/temp/pytorch-regression"

	c, err := cmd.InstantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	_, pid, _ := LookForPid(c, []string{"pytorch_regression.pid"})

	// this will always be one pid
	// never no pids since the error above accounts for that
	b.Logf("pid: %v", pid)
	c.Process.PID = pid[0]

	// We want a list of all binaries that are to be ran and benchmarked,
	// have them write their pid to temp files on disk and then have the testing suite read from them

	for i := 0; i < b.N; i++ {
		err := c.Dump(dumpDir)
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}

	b.Cleanup(
		func() {

			// Code to run after the benchmark
			// Convert the int64 value to bytes

			// TODO BS Make this dump variable just an enum...
			FileIPCCleanup(b, dumpDir, "dump")

		},
	)
}

func FileIPCCleanup(b *testing.B, dumpDir string, cmdType string) {
	os.WriteFile("../../benchmarking/temp/type", []byte(cmdType), 0o644)

	valueBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(valueBytes, uint64(b.Elapsed().Milliseconds()/int64(b.N)))

	zipFile, err := FindZipFiles(dumpDir)
	if err != nil {
		b.Errorf("Error in finding zipfile: %v", err)
	}

	filesize, err := ZipFileSize(fmt.Sprintf("%v/%v", dumpDir, zipFile))
	filesizeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(filesizeBytes, uint64(filesize))
	if err != nil {
		b.Errorf("Error in ZipFileSize(): %v", err)
	}

	err = os.WriteFile("../../benchmarking/temp/time", valueBytes, 0o644)
	if err != nil {
		b.Errorf("Error in os.WriteFile(): %v", err)
	}
	err = os.WriteFile("../../benchmarking/temp/size", filesizeBytes, 0o644)
	if err != nil {
		b.Errorf("Error in os.WriteFile(): %v", err)
	}
}

func ZipFileSize(filePath string) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	// Get the file size
	fileInfo, err := file.Stat()
	if err != nil {
		return 0, err
	}

	// Get the size
	size := fileInfo.Size()

	return size, nil
}

func LookForPid(c *cmd.Client, filename []string) ([]string, []int32, error) {

	var pidInt32s []int32
	var fileNames []string

	for _, file := range filename {

		// Open the file for reading
		dir := fmt.Sprintf("../../benchmarking/pids/%v", file)
		file, err := os.OpenFile(dir, os.O_RDONLY, 0o664)
		if err == nil {
			defer file.Close()

			// Read the bytes from the file
			var pidBytes [8]byte // Assuming int64 is 8 bytes
			_, err = file.Read(pidBytes[:])
			if err != nil {
				fmt.Println("Error reading from file:", err)
				return nil, nil, err
			}

			// Convert bytes to int32
			// LittleEndian since we are on a little endian machine,
			// x86 architecture is little endian
			pidInt32 := int32(binary.LittleEndian.Uint64(pidBytes[:]))

			// Yes we need to append because we do not know how many files there will be
			// Lots of allocations :(
			pidInt32s = append(pidInt32s, pidInt32)
			fileNames = append(fileNames, file.Name())
		}

	}

	if len(pidInt32s) == 0 {
		return nil, nil, fmt.Errorf("no pids found")
	}

	return fileNames, pidInt32s, nil
}

func GetDecompressedData(filename string) ([]byte, error) {
	dir := fmt.Sprintf("../../benchmarking/results/%v", filename)

	data, err := os.ReadFile(dir)
	if err != nil {
		return nil, err
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(data))

	if err != nil {
		return nil, err
	}

	defer gzipReader.Close()

	decompressedData, err := io.ReadAll(gzipReader)

	if err != nil {
		return nil, err
	}

	return decompressedData, nil
}

func FindZipFiles(directoryPath string) (string, error) {
	var zipFiles []string

	// Read the directory
	files, err := os.ReadDir(directoryPath)
	if err != nil {
		return "", err
	}

	// Loop through the files and check for zip files
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".zip") {
			zipFiles = append(zipFiles, file.Name())
		}
	}
	if len(zipFiles) > 1 {
		return "", fmt.Errorf("more than one zip file found")
	}

	return zipFiles[0], nil
}

func PostDumpCleanup() (*utils.Profile, *utils.Profile) {
	logger := utils.GetLogger()
	// Code to run after the benchmark
	// cpuProfileName := fmt.Sprintf("%v_cpu.prof.gz", programName)
	// memoryProfileName := fmt.Sprintf("%v_memory.prof.gz", programName)

	cpuData, err := GetDecompressedData("cpu.prof.gz")
	if err != nil {
		logger.Error().Msgf("Error in GetDecompressedData(): %v", err)
	}

	memData, err := GetDecompressedData("memory.prof.gz")
	if err != nil {
		logger.Error().Msgf("Error in GetDecompressedData(): %v", err)
	}

	cpuProfile := utils.Profile{}
	memProfile := utils.Profile{}

	proto.Unmarshal(cpuData, &cpuProfile)
	proto.Unmarshal(memData, &memProfile)

	logger.Log().Msgf("proto data duration: %+v", cpuProfile.DurationNanos)
	// Here we need to add to db the profile data
	// we also need to delete pid files and end kill processes
	return &cpuProfile, &memProfile
}

func (db *DB) CreateBenchmark(cpuProfile *utils.Profile, memProfile *utils.Profile, programName string, elapsedTime int64, fileSize int64, cmdType string) *Benchmark {
	id := xid.New()
	var timeToComplete int64
	var totalMemoryUsed int64

	// aggregate total time for cpu to run the code
	for _, sample := range cpuProfile.Sample {
		timeToComplete += sample.Value[1]
	}
	// aggregate total memory used
	for _, sample := range memProfile.Sample {
		totalMemoryUsed += sample.Value[1]
	}

	parts := strings.FieldsFunc(programName, func(r rune) bool {
		return r == '-'
	})

	// Take the first part from the split result
	prefix := parts[0]

	parts = strings.FieldsFunc(prefix, func(r rune) bool {
		return r == '/'
	})

	prefix = parts[len(parts)-1]

	cj := Benchmark{
		ID:                 id.String(),
		ProcessName:        prefix,
		TimeToCompleteInNS: timeToComplete,
		TotalMemoryUsed:    totalMemoryUsed,
		ElapsedTimeMs:      elapsedTime,
		FileSize:           fileSize,
		CmdType:            cmdType,
	}
	db.orm.Create(&cj)

	return &cj
}

func TestMain(m *testing.M) {
	// if we're in a CI environment, just exit
	if os.Getenv("CI") != "" {
		os.Exit(0)
	}

	// Only run this if we're explicitly benchmarking
	if os.Getenv("BENCHMARKING") != "" {
		os.Exit(0)
	}

	m.Run()

	finalCleanup()

}

func finalCleanup() {
	c, _ := cmd.InstantiateClient()

	pids, err := getFilenames("../../benchmarking/pids/", "")

	if err != nil {
		fmt.Printf("Error in getFilenames(): %v", err)
	}

	// Code to run after the tests
	// Profiles := Profiles{}
	cpuProfile, memProfile := PostDumpCleanup()

	db := NewDB()

	fileNames, pid, _ := LookForPid(c, pids)

	if len(fileNames) == 0 {
		return
	}

	cmdType, _ := os.ReadFile("../../benchmarking/temp/type")

	db.CreateBenchmark(cpuProfile, memProfile, fileNames[0], ReadInt64File("../../benchmarking/temp/time"), ReadInt64File("../../benchmarking/temp/size"), string(cmdType))

	if len(pid) == 0 {
		return
	}

	// Kill the processes
	for _, pid := range pid {
		process, err := os.FindProcess(int(pid))
		if err != nil {
			fmt.Println("Error finding process:", err)
		}

		// Send an interrupt signal (SIGINT) to the process
		err = process.Signal(syscall.SIGKILL)
		if err != nil {
			fmt.Println("Error sending signal:", err)
		}
	}

}

// This reads the elapsed time from a file written by benchmarking cleanup function
func ReadInt64File(filePath string) int64 {
	logger := utils.GetLogger()
	// Open the file for reading
	file, err := os.Open(filePath)
	if err != nil {
		logger.Error().Msgf("Error opening file: %v", err)
	}
	defer file.Close()

	// Read the bytes from the file
	valueBytes := make([]byte, 8)
	_, err = file.Read(valueBytes)
	if err != nil {
		logger.Error().Msgf("Error reading file: %v", err)
	}

	// Convert the bytes back to int64
	value := int64(binary.LittleEndian.Uint64(valueBytes))

	return value
}

func NewDB() *DB {
	logger := utils.GetLogger()

	originalUser := os.Getenv("SUDO_USER")
	homeDir := ""

	if originalUser != "" {
		user, err := user.Lookup(originalUser)
		if err == nil {
			homeDir = user.HomeDir
		}
	}

	if homeDir == "" {
		homeDir = os.Getenv("HOME")
	}

	configFolderPath := filepath.Join(homeDir, ".cedana")
	// check that $HOME/.cedana folder exists - create if it doesn't
	_, err := os.Stat(configFolderPath)
	if err != nil {
		logger.Log().Msg("config folder doesn't exist, creating...")
		err = os.Mkdir(configFolderPath, 0o755)
		if err != nil {
			logger.Error().Msgf("could not create config folder: %v", err)
		}
	}

	dbPath := filepath.Join(homeDir, ".cedana", "benchmarking.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		FullSaveAssociations: true,
	})
	if err != nil {
		logger.Error().Msgf("failed to open database: %v", err)
	}
	db.AutoMigrate(&Benchmark{})
	return &DB{
		orm: db,
	}
}
