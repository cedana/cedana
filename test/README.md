## Tests

### `smoke.sh`
Run smoke tests (to validate c/r works). This is a quick validation to ensure nothing is broken, and not representative of the many different things that can go wrong. 

### Benchmarking

#### Local Benchmarking

Run `entrypoint.sh` in cedana root directory with the local flag to benchmark all workloads:
```
./test/benchmarks/entrypoint.sh --local
```

This script runs the benchmark and testing suite for the dump and recovery of checkpoints in cedana. Memory and CPU profiles are measured with `pprof` and stored to `data.json` for further analysis.

#### Dockerized Benchmarking

For simplicity's sake, you can run all of this in a docker container by building the Dockerfile. To actually run it however, you need to pass the `--privileged` and mount a `tmpfs` so criu can store its intermediary files. You can do this with: 

```
docker run --privileged --tmpfs /run -it benchmark 
```

Assuming you've built the container with: 

```
docker build -t benchmark .
```
