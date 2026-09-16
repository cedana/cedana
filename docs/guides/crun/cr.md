# Checkpoint/restore crun

{% hint style="success" %}
crun is CLI-compatible with runc, so checkpoint/restore for crun containers works just like for [runc](../runc/cr.md) containers. Containers checkpointed or restored with Cedana remain fully manageable with the crun CLI (`crun state`, `crun kill`, `crun delete`, etc.).
{% endhint %}

## Prerequisites

1. Create an account with Cedana, to get access to the GPU plugin. See [authentication](../../get-started/authentication.md).
2. Set the Cedana URL & authentication token in the [configuration](../../get-started/configuration.md).
3. Install the **crun** plugin with `sudo cedana plugin install crun`.
4. Ensure the daemon is running, see [installation](../../get-started/installation.md).
5. Do a health check to ensure the plugin is ready, see [health checks](../../get-started/health.md).

## Basic

1. Run a new crun container, for example:

```sh
sudo crun run --detach --bundle ./my-bundle <container_id>
```

2. Checkpoint:

```sh
cedana dump crun <container_id> --dir <dump-dir>
```

3. Restore:

```sh
cedana restore crun --bundle ./my-bundle --path <path-to-dump>
```

## Managed

1. Run a new managed crun container:

```sh
cedana run crun --attach --jid <job_id> --bundle ./my-bundle
```

2. Checkpoint:

```sh
cedana dump job <job_id>
```

3. Restore:

```sh
cedana restore job --attach <job_id>
```

## GPU support

Just like for processes, as explained in [checkpoint/restore with GPUs](../gpu/cr.md), GPU support is also available for managed crun containers.

1. Run a new managed crun container with GPU support:

```sh
cedana run crun --attach --gpu-enabled --jid <job_id> --bundle ./my-bundle
```

2. Checkpoint:

```sh
cedana dump job <job_id>
```

3. Restore:

```sh
cedana restore job --attach <job_id>
```

## Under containerd

crun containers created through containerd (with a runtime configured with `BinaryName = "crun"`) are automatically detected and handled by the crun plugin when checkpointing/restoring through the [containerd plugin](../containerd/cr.md).

{% hint style="info" %}
For all available CLI options, see [CLI reference](../../references/cli/cedana.md). Directly interacting with daemon is also possible through gRPC, see [API reference](../../references/api.md).
{% endhint %}
