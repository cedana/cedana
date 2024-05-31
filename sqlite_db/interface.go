package sqlite_db

// This file contains the getters and setter for the sqlite DB
// Implementation of the interface can be found in the local.go

import (
	"context"
)

type DB interface {
	// Getters
	Get(context.Context, []byte) (Job, error)

	// Setters (create or update)
	Put(context.Context, []byte, []byte) error

	// Listers
	List(context.Context) ([]Job, error)
}
