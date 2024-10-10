package utils

import (
	"fmt"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
	ps "github.com/shirou/gopsutil/v3/process"
)

func GetPid(processName string) (int32, error) {
	processList, err := ps.Processes()
	if err != nil {
		return 0, err
	}

	pid, err := GetProcessSimilarity(processName, processList)
	if err != nil {
		return 0, err
	}

	return pid, nil
}

func GetProcessSimilarity(processName string, processes []*ps.Process) (int32, error) {
	similarity := 0.0
	var proc *ps.Process

	/**

	The best metric here is the Levenshtein distance (also called the edit distance). It measures the min # of
	single character edits (insertions, deletions, subs) required to transform one into another.

	It's particularily better than the other methods strutil offers w.r.t process discovery because it takes
	command variation into account, any flexibility and I think is also faster.

	**/
	lv := metrics.NewLevenshtein()

	// we don't do process discovery for containers - instead we checkpoint using the created container ID

	for _, p := range processes {
		exec, err := p.Cmdline()
		if err != nil {
			continue
		}

		// compute similarity score
		sim := strutil.Similarity(exec, processName, lv)
		if sim > similarity {
			similarity = sim
			proc = p
		}
	}

	if proc != nil {
		exec, _ := proc.Cmdline()
		fmt.Printf("found process PID %d and exe %s associated with name %s\n", proc.Pid, exec, processName)
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

// Returns a channel that will be closed when a non-child process exits
// Since, we cannot use the process.Wait() method to wait for a non-child process to exit
func WaitForPid(pid int32) chan struct{} {
	exitCh := make(chan struct{})

	go func() {
		for {
			// wait for the process to exit
			_, err := ps.NewProcess(pid)
			if err != nil {
				close(exitCh)
				return
			}
			// status, err := p.Status()
			// if err != nil {
			// 	close(exitCh)
			// return
			// }
			// fmt.Printf("%v", status)
			// time.Sleep(1 * time.Second)
		}
	}()

	return exitCh
}
