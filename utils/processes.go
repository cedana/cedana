package utils

import (
	"fmt"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
	ps "github.com/shirou/gopsutil/v3/process"
)

func GetPid(process_name string) (int32, error) {
	processList, err := ps.Processes()
	if err != nil {
		return 0, err
	}
	similarity := 0.0
	var proc *ps.Process

	for _, p := range processList {
		exec, err := p.Exe()
		if err != nil {
			continue
		}

		// compute similarity score
		sim := strutil.Similarity(exec, process_name, metrics.NewHamming())
		fmt.Printf("%s:\t%f\n", exec, sim)
		if sim > similarity {
			similarity = sim
			proc = p
		}
		return proc.Pid, nil
	}

	return 0, nil
}

func GetProcessName(pid int32) (*string, error) {
	// new process checks if exists as well
	p, err := ps.NewProcess(pid)

	if err != nil {
		return nil, err
	}

	name, _ := p.Exe()

	return &name, nil
}
