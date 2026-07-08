package storage

import (
	"fmt"

	"github.com/shirou/gopsutil/v4/process"
)

// This should not live here
// It should be filled up similiarly to how
// Query is constructured. May be it becomes
// part of Query?
// for now just add the amount of memory the process
// is using.
func EstimateCheckpointSize(pid int) (int64, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("invalid pid %d", pid)
	}

	root, err := process.NewProcess(int32(pid))
	if err != nil {
		return 0, err
	}

	var total int64
	stack := []*process.Process{root}

	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		mem, err := current.MemoryInfo()
		if err != nil {
			return 0, err
		}
		total += int64(mem.RSS)

		children, err := current.Children()
		if err != nil {
			continue
		}
		stack = append(stack, children...)
	}
	return total, nil
}
