package db

// This file contains the getters and setter for the sqlite DB
// Implementation of the interface can be found in the local.go

import (
	"context"

	"github.com/cedana/cedana/pkg/api/daemon"
)

type DB interface {
	// Getters
	GetJob(ctx context.Context, jid string) (*daemon.Job, error)

	// Setters (create or update)
	PutJob(ctx context.Context, jid string, job *daemon.Job) error

	// Listers (list jobs if no jids are provided, otherwise list the jobs with the provided jids)
	ListJobs(ctx context.Context, jids ...string) ([]*daemon.Job, error)

	// Deleters
	DeleteJob(ctx context.Context, jid string) error
}
