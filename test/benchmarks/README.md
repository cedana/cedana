## Performance

#### Getting started

The [`entrypoint.sh`](entrypoint.sh) script is the top-level to the `cedana` performance suite. Run in `cedana` root directory as
```
./test/benchmarks/entrypoint.sh [flags]
```
Default (no flags) runs all benchmarking tests, collects data with [OpenTelemetry Collector](https://github.com/open-telemetry/opentelemetry-collector) in `data.json` and `benchmark_output.csv`, processes results and pushes them to BigQuery -- used in Github Action **Benchmark and Publish**.

Flags
- `--correctness`: run correctness tests
- `--continuous`: runs continuous test
- `--local`: do not push to BigQuery
- `--smoke`: runs smoke tests -- used in Github Action **Test**

#### Dockerized Benchmarking

A [Dockerfile](Dockerfile) is provided for quick start, which also uses `entrypoint.sh`.

Build as 
```
docker build -t benchmark .
```

Run as privileged with a `tmpfs` mount
```
docker run --privileged --tmpfs /run -it benchmark 
```

