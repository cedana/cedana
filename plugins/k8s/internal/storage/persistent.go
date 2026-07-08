package storage

// Queue for making checkpoints that are completed
// but not yet in a persistent store

func (s *Storage) IsPersistented(checkpointID string) bool {
	checkpoints, ok := s.storedCheckpoints[checkpointID]
	if !ok {
		return false
	}
	return checkpoints[Persistent] != nil
}

func (s *Storage) CopyToPersistentStorage(checkpointID string) {
	// copies checkpoint to a persistent storage
	// propagates the new path to propagator
}

func (s *Storage) PersistWorker() {
	for ID := range s.toPersist {
		if !s.IsPersistented(ID) {
			s.CopyToPersistentStorage(ID)
		}
	}
}
