package db

// This file contains the getters and setter for the sqlite DB
// Implementation of the interface can be found in the local.go

import (
	"context"
	"github.com/cedana/cedana/db/models"
)

type DB interface {
	// Getters
	GetJob(context.Context, []byte) (models.Job, error)

	// Setters (create or update)
	PutJob(context.Context, []byte, []byte) error

	// Listers
	ListJobs(context.Context) ([]models.Job, error)
}
