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
│   │   ├── streamer.bats
│   │   └── runc.bats
│   ├── helpers
│   │   └── ...
└── workloads
```
Tests are grouped by functionality, and each plugin has its own test file.

Running `make help` you'll see the following test commands:
```sh
Testing
  test                      Run all tests (PARALLELISM=<n>, GPU=[0|1])
  test-unit                 Run unit tests (with benchmarks)
  test-regression           Run all regression tests (PARALLELISM=<n>, GPU=[0|1])
  test-regression-cedana    Run regression tests for cedana
  test-regression-plugin    Run regression tests for a plugin (PLUGIN=<plugin>)
  test-enter                Enter the test environment
  test-enter-cuda           Enter the test environment (CUDA)
```

When running any of the test commands locally, the tests are automatically run inside a Docker container using `cedana/cedana-test:latest` or `cedana/cedana-test:cuda` if including GPU tests. The CI is also configured to use these Docker images.

Each test command above runs the test file  **two times**, in different modes:
1. Unique daemon & DB instance for each test.
2. Single persistent daemon & DB instance across a test file.

This is to allow catching bugs that may arise due to the daemon's state being persisted across tests.

Each test command is also configured to run parallel-y, configured by the `PARALLELISM` variable passed to the test command. E.g. `make test-regression PARALLELISM=4` will run at most 4 tests in parallel at a time.

For mode 1, parallelism offers no benefits apart from fast execution time, as each test is completely isolated. However, for mode 2, parallelism may shed light on bugs in the daemon when it's handling multiple requests concurrently.
