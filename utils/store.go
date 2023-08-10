package utils

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/spf13/afero"
)

// Abstraction for storing and retreiving checkpoints
type Store interface {
	GetCheckpoint(string) (*string, error) // returns filepath to downloaded chekcpoint
	PushCheckpoint(filepath string) error
	ListCheckpoints() (*[]CheckpointMeta, error) // fix
}

type CheckpointMeta struct {
	ID       string
	Name     string
	Bucket   string
	ModTime  time.Time
	Size     uint64
	Checksum string
}

// NATS stores are tied to a job id
type NATSStore struct {
	logger *zerolog.Logger
	jsc    nats.JetStreamContext
	jobID  string
}

func (ns *NATSStore) GetCheckpoint(checkpointFilePath string) (*string, error) {
	store, err := ns.jsc.ObjectStore(strings.Join([]string{"CEDANA", ns.jobID, "checkpoints"}, "_"))
	if err != nil {
		return nil, err
	}

	downloadedFileName := "cedana_checkpoint.zip"

	err = store.GetFile(checkpointFilePath, downloadedFileName)
	if err != nil {
		return nil, err
	}

	ns.logger.Info().Msgf("downloaded checkpoint file: %s to %s", checkpointFilePath, downloadedFileName)

	// verify file exists
	// TODO NR: checksum
	_, err = os.Stat(downloadedFileName)
	if err != nil {
		ns.logger.Fatal().Msg("error downloading checkpoint file")
		return nil, err
	}

	return &downloadedFileName, nil
}

func (ns *NATSStore) PushCheckpoint(filepath string) error {
	store, err := ns.jsc.ObjectStore(strings.Join([]string{"CEDANA", ns.jobID, "checkpoints"}, "_"))
	if err != nil {
		return err
	}

	info, err := store.PutFile(filepath)
	if err != nil {
		return err
	}

	ns.logger.Info().Msgf("uploaded checkpoint file: %v", *info)

	return nil
}

func (ns *NATSStore) ListCheckpoints() (*[]CheckpointMeta, error) {
	store, err := ns.jsc.ObjectStore(strings.Join([]string{"CEDANA", ns.jobID, "checkpoints"}, "_"))
	if err != nil {
		return nil, err
	}

	var checkpoints []CheckpointMeta
	l, err := store.List()
	if err != nil {
		return nil, err
	}
	for _, metadata := range l {
		checkpoints = append(checkpoints, CheckpointMeta{
			ID:      metadata.NUID,
			Name:    metadata.Name,
			Bucket:  metadata.Bucket,
			ModTime: metadata.ModTime,
			Size:    metadata.Size,
		})
	}

	return &checkpoints, nil
}

type S3Store struct {
	logger *zerolog.Logger
}

func (s *S3Store) GetCheckpoint() (*string, error) {
	return nil, nil
}

func (s *S3Store) PushCheckpoint(filepath string) error {
	return nil
}

// For pushing and pulling from a cedana managed endpoint
type CedanaStore struct {
	logger *zerolog.Logger
	cfg    *Config
}

// ID to GetCheckpoint gets populated from the data sent over as part of a
// ServerCommand
func (cs *CedanaStore) GetCheckpoint(id string) (*string, error) {
	url := cs.cfg.Connection.CedanaUrl + "/" + "checkpoint" + "/" + id
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Bearer "+cs.cfg.Connection.CedanaAuthToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	filename := "cedana_checkpoint.zip"
	outFile, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer outFile.Close()

	// stream download to file
	// this can be parallelized if we use the server chunks - TODO NR
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		panic(err)
	}

	return &filename, nil
}

// TODO NR - multipart uploads & downloads
func (cs *CedanaStore) PushCheckpoint(filepath string) error {
	cid := uuid.New().String()

	file, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	url := cs.cfg.Connection.CedanaUrl + "/" + "checkpoint" + "/" + cid

	req, err := http.NewRequest("PUT", url, file)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("Authorization", "Bearer "+cs.cfg.Connection.CedanaAuthToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	return nil
}

func (cs *CedanaStore) ListCheckpoints() (*[]CheckpointMeta, error) {
	url := "http://localhost:1324/checkpoint"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Bearer random-user-1234-uuid-think")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var checkpointMetadata []CheckpointMeta

	// TODO NR - verify that server uses this same struct
	err = json.Unmarshal(body, &checkpointMetadata)
	if err != nil {
		return nil, err
	}

	return &checkpointMetadata, nil
}

type MockStore struct {
	fs     *afero.Afero // we can use an in-memory store for testing
	logger *zerolog.Logger
}

func (ms *MockStore) GetCheckpoint() (*string, error) {
	// gets a mock checkpoint from the local filesystem - useful for testing
	return nil, nil
}

func (ms *MockStore) PushCheckpoint(filepath string) error {
	// pushes a mock checkpoint to the local filesystem
	return nil
}

func (ms *MockStore) ListCheckpoints() (*[]CheckpointMeta, error) {
	return nil, nil
}
