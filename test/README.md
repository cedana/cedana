## Tests

### Benchmarking

Run the large_data_benchmark.sh script for a mass benchmark on all processes. Run run_benchmarks.sh script for a single run on all programs.

- To change the number data points: Modify num_iterations variable in the large_data_benchmark.sh script

run_benchmarks.sh runs the benchmark and testing suite for testing the dump and recovery of checkpoints in cedana. Memory and CPU profiles are measured with pprof and stored to a db for further analysis. Currently only one process exists, a C loop that involves high CPU utilization.

In the benchmarking directory, there are 4 sub directories: pids, processes, results, and temp.

- `pids` -> this directory stores the pids of actively running processes for benchmarking. Everything in this directory is deleted after benchmarking finishes. Pids are stored into files as int32 bytes.
- `processes` -> this directory is where docker pull pulls images of test processes.
- `results` -> this directory contains profiling results, these are overwritten each time benchmarks are ran.
- `temp` -> this is a temp directory containing dumped checkpoints. These files are used for recovery and after recovery benchmark resolves, these files are destroyed.


#### Dockerized Benchmarking

For simplicity's sake, you can run all of this in a docker container by building the Dockerfile. To actually run it however, you need to pass the `--privileged` and mount a `tmpfs` so criu can store its intermediary files. You can do this with: 

```
docker run --privileged --tmpfs /run -it benchmark 
```

Assuming you've built the container with: 

```
docker build -t benchmark .
```

### Network testing 
