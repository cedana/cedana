package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func sizeFromPath(path string) int64 {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	if err != nil {
		return 0
	}
	return size
}

func (s *Storage) reserveIfFree(priority Priority, size int64) bool {
	if s.layers[priority] == nil {
		return false
	}

	if s.layers[priority].usedLimit+size > s.layers[priority].limit {
		return false
	}

	s.layers[priority].usedLimit += size
	return true
}

func (s *Storage) findFreeLayer(size int64) (string, Priority) {
	if s.reserveIfFree(Memory, size) {
		return s.layers[Memory].path, Memory
	}

	if s.reserveIfFree(Disk, size) {
		return s.layers[Disk].path, Disk
	}

	if s.reserveIfFree(Persistent, size) {
		return s.layers[Persistent].path, Persistent
	}

	return "", -1
}

func (s *Storage) ReserveCheckpoint(pid int, checkpointID string) (string, error) {
	size, err := EstimateCheckpointSize(pid)
	if err != nil {
		return "", err
	}

	if path, priority := s.findFreeLayer(size); path != "" {
		s.storedCheckpoints[checkpointID] = [3]*Checkpoint{}
		checkpoints := s.storedCheckpoints[checkpointID]
		checkpoints[int(priority)] = &Checkpoint{
			LayerPriority: priority,
			Pid:           pid,
			CheckpointID:  checkpointID,
			Size:          size,
			unixTime:      time.Now().Unix(),
		}
		s.storedCheckpoints[checkpointID] = checkpoints
		return path, nil
	}
	return "", fmt.Errorf("could not reserve storage")
}

func (s *Storage) FinalizeCheckpoint(checkpointID string, checkpointPath string) error {
	checkpoints, ok := s.storedCheckpoints[checkpointID]
	if !ok {
		return fmt.Errorf("could not find checkpoint")
	}

	for _, checkpoint := range checkpoints {
		if checkpoint == nil {
			continue
		}
		checkpoint.Path = checkpointPath
		finalSize := sizeFromPath(checkpointPath)
		s.layers[checkpoint.LayerPriority].usedLimit += (finalSize - checkpoint.Size)
		checkpoint.Size = finalSize
		s.layers[checkpoint.LayerPriority].AddCheckpoint(checkpoint)
		s.toPersist <- checkpointID
	}
	return nil
}
