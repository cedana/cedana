# Checkpoint/restore containerd

## Prerequisites

1. Create an account with Cedana, to get access to the containerd plugin. See [authentication](../../get-started/authentication.md).
2. Set the Cedana URL & authentication token in the [configuration](../../get-started/configuration.md).
3. Install the containerd plugin with `sudo cedana plugin install containerd`.
4. Ensure the daemon is running, see [installation](../../get-started/installation.md).
5. Do a health check to ensure the plugin is ready, see [health checks](../../get-started/health.md).

## Basic

1. Run a new containerd container, for example:

```sh
sudo ctr run docker.io/library/nginx:latest <container_id>
```

2. Checkpoint:

```sh
cedana dump containerd <container_id> --dir <dump-dir>
```

3. Restore is currently WIP. However, you can restore this container as a runc container, see [checkpoint/restore runc](../runc/cr.md).

## Managed

1. Run a new managed containerd container:

```sh
cedana run containerd --attach --jid <job_id> --image docker.io/library/nginx:latest
```

2. Checkpoint:

```sh
cedana dump job <job_id>
```

3. Restore is currently WIP.

## GPU support

Just like for processes, as explained in [checkpoint/restore with GPUs](../gpu/cr.md), GPU support is also available for managed containerd containers.

1. Run a new managed containerd container with GPU support:

```sh
cedana run containerd --attach --gpu-enabled --jid <job_id> --image docker.io/library/nginx:latest
```

2. Checkpoint:

```sh
cedana dump job <job_id>
```

3. Restore is currently WIP.

## Rootfs

To include the rootfs in the checkpoint, additionally set the `--image` flag with a new image name. For example:

```sh
cedana dump containerd <container_id> --dir <dump-dir> --image <new-image-name>
```

For checkpoint _only_ the rootfs, set the `--rootfs-only` flag. For example:

```sh
cedana dump containerd <container_id> --dir <dump-dir> --image <new-image-name> --rootfs-only
```

{% hint style="info" %}
For all available CLI options, see [CLI reference](../../references/cli/cedana.md). Directly interacting with daemon is also possible through gRPC, see [API reference](../../references/api.md).
{% endhint %}
