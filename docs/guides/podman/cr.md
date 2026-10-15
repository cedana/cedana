# Checkpoint/restore podman

## Prerequisites

1. Create an account with Cedana, to get access to the podman plugin. See [authentication](../../get-started/authentication.md).
2. Set the Cedana URL & authentication token in the [configuration](../../get-started/configuration.md).
3. Install the **podman** plugin with `sudo cedana plugin install podman`.
4. Install the **oci-runtime** plugin with `sudo cedana plugin install oci-runtime`. This is the OCI runtime podman uses for containers run or restored by Cedana (see [How it works](#how-it-works)).
5. Install the lower-level plugin for podman's OCI runtime (see [Supported runtimes](#supported-runtimes)):
   - If the runtime is `crun` (default), install the **crun** plugin with `sudo cedana plugin install crun`.
   - If the runtime is `runc`, install the **runc** plugin with `sudo cedana plugin install runc`.
6. Ensure the daemon is running, see [installation](../../get-started/installation.md).
7. Do a health check to ensure the plugin is ready, see [health checks](../../get-started/health.md).

Only rootful podman is supported.

## Basic

1. Run a new podman container, for example:

```sh
sudo podman run -d --name <container> docker.io/library/nginx:latest
```

2. Checkpoint:

```sh
cedana dump podman <container> --dir <dump-dir>
```

3. Restore, as a new container (use `--id` to name it):

```sh
sudo podman rm <container>
cedana restore podman --path <dump-dir>/<dump> --id <container>
```

The restored container is a regular podman container, so `podman ps/logs/exec/stop` etc. work on it as usual.

## Managed

1. Run a new managed podman container:

```sh
cedana run podman --attach --jid <job_id> docker.io/library/nginx:latest
```

2. Checkpoint:

```sh
cedana dump job <job_id>
```

3. Restore:

```sh
cedana restore job --attach <job_id>
```

Existing (or upcoming) podman containers can also be managed with `cedana manage podman <container>`, see [managed process/container](../managed.md).

## GPU support

Just like for processes, as explained in [checkpoint/restore with GPUs](../gpu/cr.md), GPU support is also available for podman containers.

1. Run a new managed podman container with GPU support:

```sh
cedana run podman --attach --gpu-enabled --jid <job_id> <image> [command] [args...]
```

2. Checkpoint:

```sh
cedana dump job <job_id>
```

3. Restore:

```sh
cedana restore job --attach <job_id>
```

GPU restore requires podman's `overlay` storage driver (the default). CDI injects NVIDIA's driver library symlinks into the container's read/write layer when it's created, and they are only restored with it (see [read/write layer](#readwrite-layer-rootfs)).

Containers run directly with podman also get GPU support, if they are run with the Cedana OCI runtime and have NVIDIA GPUs (i.e. `NVIDIA_VISIBLE_DEVICES` is set, as in CUDA images). These can then be checkpointed with `cedana dump podman` and restored with `cedana restore podman`, as in [basic](#basic):

```sh
sudo podman run -d --runtime /usr/local/bin/cedana-oci-runtime --name <container> <cuda-image> [command] [args...]
```

To avoid passing the runtime path every time, add it to podman's [`containers.conf`](https://github.com/containers/common/blob/main/docs/containers.conf.5.md) and use `--runtime cedana`:

```toml
[engine.runtimes]
cedana = ["/usr/local/bin/cedana-oci-runtime"]
```

## How it works

Checkpointing is delegated to the plugin of the container's OCI runtime, as a rootful podman container is just a regular **crun** (or **runc**) container.

Running and restoring however must go through podman, as podman owns the container's sandbox: its network namespace, rootfs mount, and the `conmon` process that monitors it. For this, the container is created with the Cedana OCI runtime (`cedana-oci-runtime`, from the **oci-runtime** plugin), which podman invokes in place of **crun**/**runc**. It routes the container creation to `cedana run <runtime>`, or `cedana restore <runtime>` for a restore, and passes every other command through to the real runtime. This is podman's equivalent of the Cedana containerd runtime (shim).

The Cedana OCI runtime reads the following container annotations, which the podman plugin sets:

| Annotation                  | Description                                                  |
| --------------------------- | ------------------------------------------------------------ |
| `cedana.ai/runtime`         | The real runtime (`crun` or `runc`). Default: `crun` if installed |
| `cedana.ai/checkpoint-path` | Restore from this checkpoint, instead of creating            |
| `cedana.ai/criu-opts`       | CRIU options JSON for the restore                            |
| `cedana.ai/gpu`             | Enable GPU interception                                      |
| `cedana.ai/gpu-tracing`     | Enable GPU tracing                                           |

## Supported runtimes

| Runtime | Required plugins               |
| ------- | ------------------------------ |
| crun    | `crun`, `oci-runtime`          |
| runc    | `runc`, `oci-runtime`          |

The runtime is detected per-container. A checkpoint records the container's runtime, and a restore recreates the container with the same one.

## Read/write layer (rootfs)

Checkpoints automatically include the container's read/write layer (rootfs) in the dump, if podman's storage driver is `overlay` (the default). Any changes made to the container's rootfs before checkpointing will be preserved and restored. For other storage drivers (e.g. `vfs`), the read/write layer is skipped gracefully.

{% hint style="info" %}
For all available CLI options, see [CLI reference](../../references/cli/cedana.md). Directly interacting with daemon is also possible through gRPC, see [API reference](../../references/api.md).
{% endhint %}
