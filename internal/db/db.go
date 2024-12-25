package db

// This file contains the getters and setter for the sqlite DB
// Implementation of the interface can be found in the local.go

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
)

type DB interface {
	//// Job ////

	GetJob(ctx context.Context, jid string) (*daemon.Job, error)
	PutJob(ctx context.Context, jid string, job *daemon.Job) error
	ListJobs(ctx context.Context) ([]*daemon.Job, error)
	DeleteJob(ctx context.Context, jid string) error

	//// Checkpoint ////

	GetCheckpoint(ctx context.Context, id string) (*daemon.Checkpoint, error)
	CreateCheckpoint(ctx context.Context, checkpoint *daemon.Checkpoint) error
	ListCheckpoints(ctx context.Context, jid string) ([]*daemon.Checkpoint, error)
	GetLatestCheckpoint(ctx context.Context, jid string) (*daemon.Checkpoint, error)
	DeleteCheckpoint(ctx context.Context, id string) error
}
