# Documentation

Here, you will find information related to running the Cedana daemon on your machine, system architecture and the various features of daemon and CLI.

The daemon is designed to manage the lifecycle of processes/containers, including checkpoint/restore, in the larger Cedana system. Although, it can be installed and used independently as a C/R tool with its convenient defaults and a friendly command-line interface.

For detailed documentation on our managed Kubernetes or the larger Cedana system, please see [here](https://docs.cedana.ai).

## Get started
- [Quick start](#quick-start)
- [Installation](installation.md)
- [Authentication](authentication.md)
- [Configuration](configuration.md)
- [Health checks](health.md)
- [Plugins](plugins.md)
- [Feature matrix](features.md)

## Guides
- [Managed process/container](managed.md)
- [Checkpoint/restore basics](cr.md)
- [Checkpoint/restore with GPUs](gpu/cr.md)
- [Checkpoint/restore runc](runc/cr.md)
- [Checkpoint/restore containerd](runc/cr.md)
- [Checkpoint/restore kata](kata/kata.md)
- [Checkpoint/restore streamer](streamer/cr.md)

## Developer guides
- [Architecture](dev/architecture.md)
- [Profiling](dev/profiling.md)
- [Testing](dev/testing.md)
- [Writing plugins](dev/writing_plugins.md)

## References
- [CLI reference](cli/cedana.md)
- [API reference](api.md)

## Quick start

First ensure that you have Cedana installed on your machine, and the daemon is running. For installation instructions, see [installation](installation.md).

Run a new managed process:

```sh
cedana run process --attach test/workloads/date-loop.sh
```

Any process/container you spawn using `cedana run` creates a managed job. To view all managed jobs:

```sh
cedana ps
```

```
JOB               TYPE       PID  STATUS  GPU  CHECKPOINT  SIZE  LOG
personal_hopper9  process  32646  sleep   no                     [Attachable]
```

Checkpoint the job:

```sh
cedana dump job personal_hopper9
```

If you view the jobs again, you will see that it was checkpointed:

```sh
JOB               TYPE       PID  STATUS  GPU  CHECKPOINT     SIZE     LOG
personal_hopper9  process  32646  halted  no   2 seconds ago  644 KiB
```

Restore the job:

```sh
cedana restore job --attach personal_hopper9
```

For all available CLI options, see [CLI reference](cli/cedana.md). Directly interacting with daemon is also possible through gRPC, see [API reference](api.md).

For specific usage, see the guides below. For information on architecture or to get started with contributing, see the developer guides section.
