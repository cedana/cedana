package utils

import (
	"os"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/spf13/afero"
)

// Abstraction for storing and retreiving checkpoints
type Store interface {
	GetCheckpoint(string) (*string, error) // returns filepath to downloaded chekcpoint
	PushCheckpoint(filepath string) error
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

type S3Store struct {
	logger *zerolog.Logger
}

func (s *S3Store) GetCheckpoint() (*string, error) {
	return nil, nil
}

func (s *S3Store) PushCheckpoint(filepath string) error {
	return nil
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
