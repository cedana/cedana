package db

// Interface for the database

import (
	"context"
	"errors"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
)

type DB interface {
	Job
	Host
	Checkpoint
}

type Job interface {
	PutJob(ctx context.Context, job *daemon.Job) error
	ListJobs(ctx context.Context, jids ...string) ([]*daemon.Job, error)
	ListJobsByHostIDs(ctx context.Context, hostIDs ...string) ([]*daemon.Job, error)
	DeleteJob(ctx context.Context, jid string) error
}

type Host interface {
	PutHost(ctx context.Context, host *daemon.Host) error
	ListHosts(ctx context.Context, ids ...string) ([]*daemon.Host, error)
	DeleteHost(ctx context.Context, id string) error
}

type Checkpoint interface {
	PutCheckpoint(ctx context.Context, checkpoint *daemon.Checkpoint) error
	ListCheckpoints(ctx context.Context, ids ...string) ([]*daemon.Checkpoint, error)
	ListCheckpointsByJIDs(ctx context.Context, jids ...string) ([]*daemon.Checkpoint, error)
	DeleteCheckpoint(ctx context.Context, id string) error
}

/////////////////
//// Helpers ////
/////////////////

type UnimplementedDB struct{}

func (UnimplementedDB) PutJob(ctx context.Context, job *daemon.Job) error {
	return errors.New("unimplemented")
}

func (UnimplementedDB) ListJobs(ctx context.Context, jids ...string) ([]*daemon.Job, error) {
	return nil, errors.New("unimplemented")
}

func (UnimplementedDB) DeleteJob(ctx context.Context, jid string) error {
	return errors.New("unimplemented")
}

func (UnimplementedDB) PutHost(ctx context.Context, host *daemon.Host) error {
	return errors.New("unimplemented")
}

func (UnimplementedDB) ListHosts(ctx context.Context, ids ...string) ([]*daemon.Host, error) {
	return nil, errors.New("unimplemented")
}

func (UnimplementedDB) DeleteHost(ctx context.Context, id string) error {
	return errors.New("unimplemented")
}

func (UnimplementedDB) PutCheckpoint(ctx context.Context, checkpoint *daemon.Checkpoint) error {
	return errors.New("unimplemented")
}

func (UnimplementedDB) ListCheckpoints(ctx context.Context, ids ...string) ([]*daemon.Checkpoint, error) {
	return nil, errors.New("unimplemented")
}

func (UnimplementedDB) ListCheckpointsByJIDs(ctx context.Context, jids ...string) ([]*daemon.Checkpoint, error) {
	return nil, errors.New("unimplemented")
}

func (UnimplementedDB) DeleteCheckpoint(ctx context.Context, id string) error {
	return errors.New("unimplemented")
}
