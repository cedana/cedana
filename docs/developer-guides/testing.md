## Testing

Tests are configured to run inside a Docker container, ridding the need for tedious setup scripts. This allows for testing locally, during development, without affecting the environment.

The test directory looks like this:

```
test
├── regression
│   ├── basic.bats
│   ├── cr.bats
│   ├── run.bats
│   ├── manage.bats
│   ├── profiling.bats
│   ├── plugins
│   │   ├── containerd.bats
│   │   ├── crio.bats
│   │   ├── gpu.bats
│   │   ├── gpu_runc.bats
│   │   ├── gpu_streamer.bats
│   │   ├── streamer.bats
│   │   ├── streamer_runc.bats
│   │   └── runc.bats
│   ├── helpers
│   │   └── ...
└── workloads
```

Tests are grouped by functionality, and each plugin has its own test file.

### Running tests

Running `make help` you'll see the following test commands:

```sh
Testing
  test                      Run all tests (PARALLELISM=<n>, GPU=[0|1], TAGS=<tags>, RETRIES=<retries>, DEBUG=[0|1])
  test-unit                 Run unit tests (with benchmarks)
  test-regression           Run regression tests (PARALLELISM=<n>, GPU=[0|1], TAGS=<tags>, RETRIES=<retries>, DEBUG=[0|1])
  test-k8s                  Run kubernetes e2e tests (PARALLELISM=<n>, GPU=[0|1], TAGS=<tags>, RETRIES=<retries>, DEBUG=[0|1], ...)
  test-enter                Enter the test environment
  test-enter-cuda           Enter the test environment (CUDA)
  test-k9s                  Enter k9s in the test environment

```

When running any of the test commands locally, the tests are automatically run inside a Docker container using `cedana/cedana-test:latest` or `cedana/cedana-test:cuda` if `GPU=1`. GPU tests are skipped by default, unless `GPU=1` is set.

### Running GPU tests

Use `GPU=1` to include all GPU tests. If `GPU=0`, any tests that require GPU-support automatically get skipped. You may want to specify a low `PARALLELISM` value when running GPU tests, as each test requires a significant amount of RAM.

### Running specific tests

Use `TAGS` to filter tests by tags. For example, `make test-regression TAGS=runc` will run all tests tagged with `runc`. `make test-regression TAGS=runc,gpu` will run all tests tagged with `runc` and `gpu`. If `gpu` tag is included, you must set `GPU=1` to run the tests, otherwise they will be skipped.

### Test modes

Each test command above runs the test suite **two times**, in different modes:

1. Unique daemon & DB instance for each test.
2. Single persistent daemon & DB instance across a test suite.

This is to allow catching bugs that may arise due to the daemon's state being persisted across tests.

Each test command is also configured to run parallel-y, configured by the `PARALLELISM` variable passed to the test command. E.g. `make test-regression PARALLELISM=4` will run at most 4 tests in parallel at a time.

For mode 1, parallelism offers no benefits apart from fast execution time, as each test is completely isolated. However, for mode 2, parallelism may shed light on bugs in the daemon when it's handling multiple requests concurrently.
