# Cedana

![GitHub Release](https://img.shields.io/github/v/release/cedana/cedana) ![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/cedana/cedana/release.yml?branch=main)

![output-onlinepngtools](https://github.com/user-attachments/assets/8f7930c0-8cef-451f-bb96-a30625dc690b)

Welcome to Cedana! This repository is the home of the Cedana daemon and the low-level orchestration of our save/migrate/resume (SMR) functionality, and is the entry-point into the larger cedana ecosystem.

We build on top of and leverage [CRIU](https://github.com/checkpoint-restore/criu) to provide userspace checkpoint/restore of processes and the many different abstraction levels that lie above. We also provide the ability to checkpoint/restore rootfs in both containerd and CRIO interfaces for full container checkpoint/restores.

For a list of supported container runtimes, see [features](https://docs.cedana.ai/daemon/get-started/features).

We can monitor, migrate and automate checkpoints across a real-time network and compute configuration enabling ephemeral and hardware agnostic compute. See [our website](https://cedana.ai) for more information about our managed product.

Some problems Cedana can help solve include:

- Cold-starts for containers & processes
- Keeping a process or container running independent of hardware/network failure
- Managing multiprocess/multinode systems (independent of Kubernetes/SLURM or any orchestration)
- Kubernetes checkpoint/restore
- GPU checkpoint/restore
- And more!

## Documentation

To get started using Cedana locally, check out the [documentation](https://docs.cedana.ai/daemon).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and developer guides in the [documentation](https://docs.cedana.ai/daemon).
