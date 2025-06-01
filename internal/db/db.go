package db

// Interface for the database

import (
	"context"
	"errors"

	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
)

type DB interface {
	Job
	Host
	Checkpoint
	GPU
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

type GPU interface {
	PutGPUController(ctx context.Context, controller *GPUController) error
	ListGPUControllers(ctx context.Context, ids ...string) ([]*GPUController, error)
	DeleteGPUController(ctx context.Context, id string) error
}

/////////////////
//// Helpers ////
/////////////////

type GPUController struct {
	ID          string
	Address     string
	PID         uint32
	AttachedPID uint32
	FreezeType  gpu.FreezeType
}

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

func (UnimplementedDB) ListCheckpointsByJID(ctx context.Context, jids ...string) ([]*daemon.Checkpoint, error) {
	return nil, errors.New("unimplemented")
}

func (UnimplementedDB) DeleteCheckpoint(ctx context.Context, id string) error {
	return errors.New("unimplemented")
}

func (UnimplementedDB) PutGPUController(ctx context.Context, controller *GPUController) error {
	return errors.New("unimplemented")
}

func (UnimplementedDB) ListGPUControllers(ctx context.Context, ids ...string) ([]*GPUController, error) {
	return nil, errors.New("unimplemented")
}

func (UnimplementedDB) DeleteGPUController(ctx context.Context, id string) error {
	return errors.New("unimplemented")
}
