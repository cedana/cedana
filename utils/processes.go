package utils

import (
	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"

	ps "github.com/mitchellh/go-ps"
)

func GetPid(process_name string) (int, error) {
	processList, err := ps.Processes()
	if err != nil {
		return 0, err
	}
	// brittle search, TODO make more robust, need to be able to grab a 
	// range of processes too (here assuming root process is most similar)
	similarity := 0.0
	var proc ps.Process
	for x := range processList {
		process := processList[x]
		sim := strutil.Similarity(process.Executable(), process_name, metrics.NewHamming())
		if sim > similarity {
			similarity = sim
			proc = process
		}
	}
	return proc.Pid(), nil
}
