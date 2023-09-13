package test

// We use Input to input variance into the program/process being run.
// Therefore, we expect that the executed script takes flag input and can correctly
// process it.
type ProcessIOExpectedOutput struct {
	Output []interface{}
}

type Benchmark struct {
	name         string // name of the process
	exec         string // path to exec
	ProcessIOMap map[interface{}]ProcessIOExpectedOutput
}

func createBenchmarks() []Benchmark {
	return []Benchmark{
		{
			name: "loop",
			exec: "./benchmarking/processes/loop",
		},
	}
}
