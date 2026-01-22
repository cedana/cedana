package job

// Manager is the interface that defines the methods that a job manager should implement.
// Most methods here cannot fail or return error, forcing implmentations to manage state
// this in-memory.

import (
	"context"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
)

type Manager interface {
	//////////////
	//// Jobs ////
	//////////////

	// New creates a new job with the given details.
	New(jid string, jobType string) (*Job, error)

	// Get returns a job with the given JID.
	Get(jid string) *Job

	// Delete deletes a job with the given JID.
	Delete(jid string)

	// Get returns a job with the given JID.
	List(jids ...string) []*Job

	// ListByHostIDs returns a list of jobs with the given host IDs.
	ListByHostIDs(hostIDs ...string) []*Job

	// Exists checks if a job with the given JID exists.
	Exists(jid string) bool

	// Find returns a job with the given PID.
	Find(pid uint32) *Job

	// Starts managing a running job, updating state once it exits.
	// Since this runs in background, it should be called with a waitgroup,
	// to ensure the caller can wait for the job to finish. If no exited channel is given,
	// uses the PID to wait for the job to exit.
	Manage(lifetime context.Context, jid string, pid uint32, code <-chan int) error

	// Kill sends a signal to a job with the given JID.
	// If the plugin for the job type exports a custom signal, it will be used instead.
	// If you provide a custom signal, it will return error if the plugin for the job type
	// exports a custom signal.
	Kill(jid string, signal ...syscall.Signal) error

	/////////////////////
	//// Checkpoints ////
	/////////////////////

	// AddCheckpoint adds a checkpoint path to the job.
	AddCheckpoint(jid string, paths []string)

	// Get a specific checkpoint.
	GetCheckpoint(id string) *daemon.Checkpoint

	// GetCheckpoints returns the saved checkpoints for the job.
	ListCheckpoints(jid string) []*daemon.Checkpoint

	// GetLatestCheckpoint returns the latest checkpoint for the job.
	GetLatestCheckpoint(jid string) *daemon.Checkpoint

	// DeleteCheckpoint deletes a checkpoint with the given ID.
	DeleteCheckpoint(id string)

	//////////////
	//// Misc ////
	//////////////

	// Sync is used to force the manager to sync its state with the underlying storage.
	Sync(ctx context.Context) error
}
