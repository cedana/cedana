---
layout:
  title:
    visible: false
  description:
    visible: false
  tableOfContents:
    visible: true
  outline:
    visible: true
  pagination:
    visible: true
---

# Cedana Daemon

<figure><img src=".gitbook/assets/image (1).png" alt=""><figcaption></figcaption></figure>

Here, you will find information on running the Cedana daemon on your machine, system architecture, and the various features of both the daemon and CLI.

The daemon is designed to manage the lifecycle of processes/containers, including checkpoint/restore, in the larger Cedana system. However, it can be installed and used independently as a checkpoint/restore tool with its convenient defaults and a friendly command-line interface.

{% hint style="info" %}
For detailed documentation on our managed Kubernetes or the larger Cedana system, please see [here](https://docs.cedana.ai).
{% endhint %}

### Quick start

First, ensure that you have Cedana installed on your machine, and the daemon is running. See [installation](get-started/installation.md).

#### Run a new job

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

#### Checkpoint the job

```sh
cedana dump job personal_hopper9
```

If you view the jobs again, you will see that it was checkpointed:

```sh
JOB               TYPE       PID  STATUS  GPU  CHECKPOINT     SIZE     LOG
personal_hopper9  process  32646  halted  no   2 seconds ago  644 KiB
```

#### Restore the job

```sh
cedana restore job --attach personal_hopper9
```

For specific usage, check out the [guides](./#guides). For information on architecture or to get started with contributing, check out the [developer guides](./#developer-guides).

{% hint style="info" %}
For all available CLI options, see [CLI reference](references/cli/cedana.md). Directly interacting with daemon is also possible through gRPC, see [API reference](references/api.md).
{% endhint %}

### Get started

- [Quick start](./#quick-start)
- [Installation](get-started/installation.md)
- [Authentication](get-started/authentication.md)
- [Configuration](get-started/configuration.md)
- [Health checks](get-started/health.md)
- [Plugins](get-started/plugins.md)
- [Features](get-started/features.md)

### Checkpoint/restore guides

- [Managed process/container](guides/managed.md)
- [Checkpoint/restore basics](guides/cr.md)
- [Checkpoint/restore with GPUs](guides/gpu/cr.md)
- [Checkpoint/restore runc](guides/runc/cr.md)
- [Checkpoint/restore containerd](guides/runc/cr.md)
- [Checkpoint/restore streamer](guides/streamer/cr.md)
- [Checkpoint/restore kubernetes](guides/k8s/cr.md)

### Storage guides

- [Amazon S3](guides/storage/s3.md)
- [Google Cloud Storage](guides/storage/gcs.md)
- [Cedana Storage](guides/storage/cedana.md)

### Developer guides

- [Architecture](developer-guides/architecture.md)
- [Profiling](developer-guides/profiling.md)
- [Testing](developer-guides/testing.md)
- [Writing plugins](developer-guides/writing_plugins.md)

### References

- [CLI reference](references/cli/cedana.md)
- [API reference](references/api.md)
