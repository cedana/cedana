## Tests

### `testdir/proc`

Wrangled some live proc data to test against. Should consider using filesystem mock in the future, but this is a quick and dirty way to run some tests. Should also consider pruning these for the future. For reference:

- `1266999` -> is a process spawned by running `jupyter notebook &` (useful for testing interactive programs & python restores)
- `1227709` -> is a process spawned by running `./server -m models/7B/ggml-model-q4_0.bin -c 2048 &` (useful for testing servers & network restores)

### `large_data_benchmark.sh`

Run the large_data_benchmark.sh script for a mass benchmark on all processes. Run run_benchmarks.sh script for a single run on all programs.

- To change the number data points: Modify num_iterations variable in the large_data_benchmark.sh script

run_benchmarks.sh runs the benchmark and testing suite for testing the dump and recovery of checkpoints in cedana. Memory and CPU profiles are measured with pprof and stored to a db for further analysis. Currently only one process exists, a C loop that involves high CPU utilization.

In the benchmarking directory, there are 4 sub directories: pids, processes, results, and temp.

- `pids` -> this directory stores the pids of actively running processes for benchmarking. Everything in this directory is deleted after benchmarking finishes. Pids are stored into files as int32 bytes.
- `processes` -> this directory is where docker pull pulls images of test processes.
- `results` -> this directory contains profiling results, these are overwritten each time benchmarks are ran.
- `temp` -> this is a temp directory containing dumped checkpoints. These files are used for recovery and after recovery benchmark resolves, these files are destroyed.
