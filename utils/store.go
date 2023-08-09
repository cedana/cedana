package utils

// Abstraction for storing and retreiving checkpoints
type Store interface {
	GetCheckpoint() (*string, error) // returns filepath to downloaded chekcpoint
	PushCheckpoint(filepath string) error
}

type NATSStore struct {
}

func (ns *NATSStore) GetCheckpoint() (*string, error) {
	return nil, nil
}

func (ns *NATSStore) PushCheckpoint(filepath string) error {
	return nil
}

type S3Store struct {
}

func (s *S3Store) GetCheckpoint() (*string, error) {
	return nil, nil
}

func (s *S3Store) PushCheckpoint(filepath string) error {
	return nil
}
