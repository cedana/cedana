# Cedana

![GitHub Release](https://img.shields.io/github/v/release/cedana/cedana) ![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/cedana/cedana/release.yml?branch=main)

Welcome to Cedana! This repository is the home of the Cedana daemon and the low-level orchestration of our save/migrate/resume (SMR) functionality, and is the entry-point into the larger cedana ecosystem.

We build on top of and leverage [CRIU](https://github.com/checkpoint-restore/criu) to provide userspace checkpoint/restore of processes and the many different abstraction levels that lie above. We also provide the ability to checkpoint/restore rootfs in both containerd and CRIO interfaces for full container checkpoint/restores.

For a list of supported container runtimes, see our [runtime support matrix](docs/support/runtimes.md).

We can monitor, migrate and automate checkpoints across a real-time network and compute configuration enabling ephemeral and hardware agnostic compute. See [our website](https://cedana.ai) for more information about our managed product.

Some problems Cedana can help solve include:

- Cold-starts for containers & processes
- Keeping a process or container running independent of hardware/network failure
- Managing multiprocess/multinode systems (independent of Kubernetes/SLURM or any orchestration)
- Kubernetes checkpoint/restore
- GPU checkpoint/restore
- And more!

## Documentation

To get started using Cedana locally, check out the [docs](docs/README.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.
