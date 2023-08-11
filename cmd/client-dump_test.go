package cmd

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/nravic/cedana/utils"
	"github.com/rs/xid"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

type DB struct {
	orm *gorm.DB
}

type Benchmarks struct {
	gorm.Model
	ID                 string `gorm:"primaryKey"`
	ProcessName        string
	TimeToCompleteInNS int64
	TotalMemoryUsed    int64
}

func BenchmarkDumpLoop(b *testing.B) {
	c, err := instantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	_, pid, err := LookForPid(c, []string{"loop.pid"})
	if err != nil {
		b.Errorf("Error in LookForPid(): %v", err)
	}

	c.process.PID = pid[0]

	// We want a list of all binaries that are to be ran and benchmarked,
	// have them write their pid to temp files on disk and then have the testing suite read from them

	for i := 0; i < b.N; i++ {
		err := c.dump("../benchmarking/temp/")
		if err != nil {
			b.Errorf("Error in dump(): %v", err)
		}
	}

}

func BenchmarkDumpServer(b *testing.B) {
	c, err := instantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	_, pid, err := LookForPid(c, []string{"server.pid"})

	if err != nil {
		b.Errorf("Error in LookForPid(): %v", err)
	}
	// this will always be one pid
	// never no pids since the error above accounts for that
	c.process.PID = pid[0]

	// We want a list of all binaries that are to be ran and benchmarked,
	// have them write their pid to temp files on disk and then have the testing suite read from them

	for i := 0; i < b.N; i++ {
		err := c.dump("../benchmarking/temp/server")
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

func LookForPid(c *Client, filename []string) ([]string, []int32, error) {

	var pidInt32s []int32
	var fileNames []string

	for _, file := range filename {

		// Open the file for reading
		dir := fmt.Sprintf("../benchmarking/pids/%v", file)
		file, err := os.Open(dir)
		if err != nil {
			fmt.Println("Error opening file:", err)
		} else {
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
	dir := fmt.Sprintf("../benchmarking/results/%v", filename)

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

func PostDumpCleanup() (*utils.Profile, *utils.Profile) {
	c, _ := instantiateClient()
	// Code to run after the benchmark
	cpuData, err := GetDecompressedData("cpu.prof.gz")
	if err != nil {
		c.logger.Error().Msgf("Error in GetDecompressedData(): %v", err)
	}

	memData, err := GetDecompressedData("memory.prof.gz")
	if err != nil {
		c.logger.Error().Msgf("Error in GetDecompressedData(): %v", err)
	}

	cpuProfile := utils.Profile{}
	memProfile := utils.Profile{}

	proto.Unmarshal(cpuData, &cpuProfile)
	proto.Unmarshal(memData, &memProfile)

	c.logger.Log().Msgf("proto data duration: %+v", cpuProfile.DurationNanos)
	// Here we need to add to db the profile data
	// we also need to delete pid files and end kill processes
	return &cpuProfile, &memProfile
}

func (db *DB) CreateBenchmark(cpuProfile *utils.Profile, memProfile *utils.Profile, programName string) *Benchmarks {
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

	cj := Benchmarks{
		ID:                 id.String(),
		ProcessName:        programName,
		TimeToCompleteInNS: timeToComplete,
		TotalMemoryUsed:    totalMemoryUsed,
	}
	db.orm.Create(&cj)

	return &cj
}

func TestMain(m *testing.M) {
	// Code to run before the tests
	c, _ := instantiateClient()
	pids := []string{"loop.pid", "server.pid"}
	m.Run()
	// Code to run after the tests
	cpuProfile, memProfile := PostDumpCleanup()

	db := NewDB()

	fileNames, pid, _ := LookForPid(c, pids)
	// TODO BS Figure out how to get the name of the process
	db.CreateBenchmark(cpuProfile, memProfile, fileNames[0])

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

func NewDB() *DB {
	c, err := instantiateClient()
	if err != nil {
		c.logger.Error().Msgf("Error in instantiateClient(): %v", err)
	}

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
	_, err = os.Stat(configFolderPath)
	if err != nil {
		c.logger.Log().Msg("config folder doesn't exist, creating...")
		err = os.Mkdir(configFolderPath, 0o755)
		if err != nil {
			c.logger.Error().Msgf("could not create config folder: %v", err)
		}
	}

	dbPath := filepath.Join(homeDir, ".cedana", "benchmarking.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		FullSaveAssociations: true,
	})
	if err != nil {
		c.logger.Error().Msgf("failed to open database: %v", err)
	}
	db.AutoMigrate(&Benchmarks{})
	return &DB{
		orm: db,
	}
}
