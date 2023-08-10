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
	ProcessID          int32
	TimeToCompleteInNS int64
}

func BenchmarkDump(b *testing.B) {
	c, err := instantiateClient()

	if err != nil {
		b.Errorf("Error in instantiateClient(): %v", err)
	}

	for {
		// TODO BS Need to add time out here
		filename, err := LookForPid()
		if err != nil {
			b.Errorf("Error in LookForPid(): %v", err)
		}
		if filename != "" {
			// Open the file for reading
			file, err := os.Open("../benchmarking/pids/pid-loop.txt")
			if err != nil {
				fmt.Println("Error opening file:", err)
				return
			}
			defer file.Close()

			// Read the bytes from the file
			var pidBytes [8]byte // Assuming int64 is 8 bytes
			_, err = file.Read(pidBytes[:])
			if err != nil {
				fmt.Println("Error reading from file:", err)
				return
			}

			// Convert bytes to int32
			pidInt32 := int32(binary.LittleEndian.Uint64(pidBytes[:]))
			c.process.PID = pidInt32
			break
		}
	}

	// We want a list of all binaries that are to be ran and benchmarked,
	// have them write their pid to temp files on disk and then have the testing suite read from them

	for i := 0; i < b.N; i++ {
		err := c.dump("../benchmarking/temp/")
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

func LookForPid() (string, error) {
	dirPath := "../benchmarking/pids/"

	// Open the directory
	dir, err := os.Open(dirPath)
	if err != nil {
		fmt.Println("Error opening directory:", err)
		return "", err
	}
	defer dir.Close()

	// Read the directory contents
	fileInfos, err := dir.Readdir(-1)
	if err != nil {
		fmt.Println("Error reading directory contents:", err)
		return "", err
	}

	// Iterate over the files
	for _, fileInfo := range fileInfos {
		if fileInfo.Mode().IsRegular() {
			return fileInfo.Name(), err
		}
	}
	err = fmt.Errorf("No files found in directory")
	return "", err
}

func PostDumpCleanup() {
	c, _ := instantiateClient()
	// Code to run after the benchmark
	data, err := os.ReadFile("../benchmarking/results/cpu.prof.gz")
	// len of data is 0 for some reason

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

	profile := utils.Profile{}

	proto.Unmarshal(decompressedData, &profile)

	c.logger.Log().Msgf("proto data duration: %+v", profile.DurationNanos)
	// Here we need to add to db the profile data
	// we also need to delete pid files and end kill processes
}

func TestMain(m *testing.M) {
	// Code to run before the tests

	m.Run()
	// Code to run after the tests
	PostDumpCleanup()
	NewDB()
}

func NewDB() *DB {
	c, err := instantiateClient()
	if err != nil {
		c.logger.Error().Msgf("Error in instantiateClient(): %v", err)
	}

	// this is the same code that's sitting in bootstrap right now
	// safe to assume that bootstrap has been run (but also safe to assume it hasn't?)

	// See if this works around the issue of the db not being created
	// sudo env is different.
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

func (db *DB) CreateJob(profile *utils.Profile, pid int32) *Benchmarks {
	id := xid.New()
	cj := Benchmarks{
		ID:                 id.String(),
		ProcessName:        "loop",
		ProcessID:          pid,
		TimeToCompleteInNS: profile.DurationNanos,
	}
	db.orm.Create(&cj)

	return &cj
}
