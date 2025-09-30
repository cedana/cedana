# Checkpoint/restore runc

## Prerequisites

1. Create an account with Cedana, to get access to the GPU plugin. See [authentication](../../get-started/authentication.md).
2. Set the Cedana URL & authentication token in the [configuration](../../get-started/configuration.md).
3. Install the runc plugin with `sudo cedana plugin install runc`.
4. Ensure the daemon is running, see [installation](../../get-started/installation.md).
5. Do a health check to ensure the plugin is ready, see [health checks](../../get-started/health.md).

## Basic

1. Run a new runc container, for example:

```sh
sudo runc run --detach <container_id> --bundle ./my-bundle
```

2. Checkpoint:

```sh
cedana dump runc <container_id> --dir <dump-dir>
```

3. Restore:

```sh
cedana restore runc --bundle ./my-bundle --path <path-to-dump>
```

## Managed

1. Run a new managed runc container:

```sh
cedana run runc --attach --jid <job_id> --bundle ./my-bundle
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

Just like for processes, as explained in [checkpoint/restore with GPUs](../gpu/cr.md), GPU support is also available for managed runc containers.

1. Run a new managed runc container with GPU support:

```sh
cedana run runc --attach --gpu-enabled --jid <job_id> --bundle ./my-bundle
```

2. Checkpoint:

```sh
cedana dump job <job_id>
```

3. Restore:

```sh
cedana restore job --attach <job_id>
```

{% hint style="info" %}
For all available CLI options, see [CLI reference](../../references/cli/cedana.md). Directly interacting with daemon is also possible through gRPC, see [API reference](../../references/api.md).
{% endhint %}
