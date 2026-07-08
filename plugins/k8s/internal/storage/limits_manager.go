package storage

// Background process that deletes
// checkpoints in non-persistent storage
// in order to free up space for upcoming checkpoints
// based on the priority

const (
	MEM_CACHE_UTILIZATION_TARGET  = 0.6
	DISK_CACHE_UTILIZATION_TARGET = 0.9
)

func (s *Storage) CleanupCheckpoint(checkpointID string) {
	// don't cleanup not persistented checkpoints
	if !s.IsPersistented(checkpointID) {
		return
	}

	// Call out to propagator that this node would like
	// to cleanup checkpointID from non-persistent storage

	// propagator confirms there are no ongoing restores so
	// clean it up

	// CLEAN UP
	// 1. Moving From mem to disk
	// 2. Removing from disk

	// Update propagator that it has been cleaned up
}

func (s *Storage) CleanupWorker() {
	// worker that continously runs to cleanup up from heap
	// and ensure that UTILIZATION_TARGETS are met
}
