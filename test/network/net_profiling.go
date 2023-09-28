package test

type NetBenchmark struct {
	Name string
	Exec string
}

type NetBenchmarkData struct {
	NumSockets     int
	NumConnections int
	CheckpointTime int
	RestoreTime    int
}

var netBenchmarks = map[string]NetBenchmark{
	"multiconn": {"threaded_pings", "python3 ../../cedana-benchmarks/networking/threaded_pings.py -n 3 google.com 80"},
}

// exec program inside of cedana

// collect data
// save data somewhere
