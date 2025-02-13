# Documentation

Here, you will find information related to running the Cedana daemon on your machine, system architecture and the various features of daemon and CLI.

The daemon is designed to manage the lifecycle of processes/containers, including checkpoint/restore, in the larger Cedana system. Although, it can be installed and used independently as a C/R tool with its convenient defaults and a friendly command-line interface.

For detailed documentation on our managed Kubernetes or the larger Cedana system, please see [here](https://docs.cedana.ai).

## Quick start

The simplest demonstration of checkpoint/restore can be performed on your machine.

Start the daemon (requires root privileges):

```sh
sudo cedana daemon start
```

Run a new managed process:

```sh
cedana run process --attach test/workloads/date-loop.sh
```

Any process/container you spawn using `cedana run` creates a managed job. To view all managed jobs:

```sh
cedana ps
```

```sh
JOB               TYPE       PID  STATUS  GPU  CHECKPOINT  SIZE  LOG
personal_hopper9  process  32646  sleep   no                     [Attachable]
```

To checkpoint the job:

```sh
cedana dump job personal_hopper9
```

If you view the jobs again, you will see that it was checkpointed:

```sh
JOB               TYPE       PID  STATUS  GPU  CHECKPOINT     SIZE     LOG
personal_hopper9  process  32646  halted  no   2 seconds ago  644 KiB
```

To restore:

```sh
cedana restore job --attach personal_hopper9
```

For all available CLI options, see [CLI reference](cli/cedana.md).

For more advanced usage, see the guides below. For information on architecture or to get started with contributing, see the developer guides section.

## Guides

- [CLI reference](cli/cedana.md)
- [API reference](api.md)
- [Checkpoint/restore with GPUs](gpu/cr.md)
- [Checkpoint/restore kata](kata/kata.md)
- [Checkpoint/restore runc](runc/cr.md)
- [Checkpoint/restore containerd](runc/cr.md)
- [Checkpoint/restore streamer](streamer/cr.md)

## Developer guides

- [System architecture](dev/architecture.md)
- [Feature matrix](dev/features.md)
- [Writing plugins](dev/plugins.md)
