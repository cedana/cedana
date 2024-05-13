package db

// This file contains the key-value database interface
// The interface can be implemented as a local/remote KV database

// Opening/creation of files/buckets must be managed by implementation
// [][]byte is used to represent a path to a bucket (for nested buckets)
type DB interface {
	// Getters
	Get([][]byte, []byte) ([]byte, error)
	GetString([][]byte, []byte) (string, error)
	GetBool([][]byte, []byte) (bool, error)
	GetInt([][]byte, []byte) (int, error)

	// Setters (create or update)
	Put([][]byte, []byte, []byte) error
	PutString([][]byte, []byte, string) error
	PutBool([][]byte, []byte, bool) error
	PutInt([][]byte, []byte, int) error

	// Listers
	List([][]byte) ([][]byte, error)
	ListKeys([][]byte) ([][]byte, error)
}
