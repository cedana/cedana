package storage

import (
	"fmt"
	"os"
	"path/filepath"
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

func (s *Storage) ReserveCheckpoint(pid int) (string, error) {
	size, err := EstimateCheckpointSize(pid)
	if err != nil {
		return "", err
	}

	if path, priority := s.findFreeLayer(size); path != "" {
		s.inProgressCheckpoints[pid] = Checkpoint{
			LayerPriority: priority,
			Size:          size,
		}
		return path, nil
	}
	return "", fmt.Errorf("could not reserve storage")
}

func (s *Storage) FinalizeCheckpoint(pid int, checkpointPath string) error {
	checkpoint, ok := s.inProgressCheckpoints[pid]
	if !ok {
		return fmt.Errorf("expected to find in progress checkpoint for pid")
	}

	finalSize := sizeFromPath(checkpointPath)
	s.layers[checkpoint.LayerPriority].usedLimit += (finalSize - checkpoint.Size)

	s.storedCheckpoints[pid] = Checkpoint{
		Size:          finalSize,
		LayerPriority: checkpoint.LayerPriority,
		Pid:           pid,
		Path:          checkpointPath,
	}
	return nil
}
