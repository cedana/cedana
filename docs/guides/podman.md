# Checkpoint/restore podman

## Prerequisites

1. Create an account with Cedana, to get access to the podman plugin. See [authentication](../get-started/authentication.md).
2. Set the Cedana URL & authentication token in the [configuration](../get-started/configuration.md).
3. Install the **podman** plugin with `sudo cedana plugin install podman`.
4. Install the **oci-runtime** plugin with `sudo cedana plugin install oci-runtime`. This is the OCI runtime podman uses for containers run or restored by Cedana (see [How it works](#how-it-works)).
5. Install the lower-level plugin for podman's OCI runtime (see [Supported runtimes](#supported-runtimes)):
   - If the runtime is `crun` (default), install the **crun** plugin with `sudo cedana plugin install crun`.
   - If the runtime is `runc`, install the **runc** plugin with `sudo cedana plugin install runc`.
6. Ensure the daemon is running, see [installation](../get-started/installation.md).
7. Do a health check to ensure the plugin is ready, see [health checks](../get-started/health.md).

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

Existing (or upcoming) podman containers can also be managed with `cedana manage podman <container>`, see [managed process/container](managed.md).

## GPU support

Just like for processes, as explained in [checkpoint/restore with GPUs](gpu.md), GPU support is also available for podman containers.

1. Run a new managed podman container with GPU support:

```sh
cedana run podman --attach --gpu-enabled --gpus <gpus> --jid <job_id> <image> [command] [args...]
```

`--gpus` adds the given GPUs to the container (e.g. `--gpus 0,1`), like podman's `--gpus`.

2. Checkpoint:

```sh
cedana dump job <job_id>
```

3. Restore:

```sh
cedana restore job --attach <job_id>
```

The restored container gets the same GPUs as the job was run with.

GPU restore requires podman's `overlay` storage driver (the default). CDI injects NVIDIA's driver library symlinks into the container's read/write layer when it's created, and they are only restored with it (see [read/write layer](#readwrite-layer-rootfs)).

For GPU containers run directly with podman, see [Using the Cedana OCI runtime directly](#using-the-cedana-oci-runtime-directly).

## Using the Cedana OCI runtime directly

Instead of running containers through Cedana (`cedana run podman`), you can keep using podman as usual and just switch its OCI runtime to the Cedana OCI runtime (`cedana-oci-runtime`, from the **oci-runtime** plugin). Such containers can be checkpointed and restored, including GPU containers, and remain regular podman containers in every other way.

Containers run with podman's default runtime (`crun`/`runc`) can also be checkpointed as in [basic](#basic), but **only containers run with the Cedana OCI runtime get GPU support**, as GPU interception must be set up when the container is created.

### Setup

Add the runtime to podman's [`containers.conf`](https://github.com/containers/common/blob/main/docs/containers.conf.5.md) (e.g. `/etc/containers/containers.conf`):

```toml
[engine.runtimes]
cedana = ["/usr/local/bin/cedana-oci-runtime"]
```

Then use `--runtime cedana` with `podman run`/`podman create`. Alternatively, pass the full path every time: `--runtime /usr/local/bin/cedana-oci-runtime`. To use it for all containers, also set `runtime = "cedana"` under `[engine]`.

### Run

```sh
sudo podman run -d --runtime cedana --name <container> <image> [command] [args...]
```

The container runs on the real runtime (`crun`, or `runc` if `crun` is not installed). To choose one, add `--annotation cedana.ai/runtime=runc` (see [annotations](#annotations)).

### Checkpoint

```sh
cedana dump podman <container> --dir <dump-dir>
```

The container exits after the checkpoint, unless `--leave-running` is passed.

### Restore

Create a container from the same image with the `cedana.ai/checkpoint-path` annotation:

```sh
sudo podman rm <container>
sudo podman run -d --runtime cedana --annotation cedana.ai/checkpoint-path=<dump-dir>/<dump> --name <container> <image>
```

Instead of starting the image's command, the Cedana OCI runtime restores the checkpointed process and read/write layer into the new container.

### GPU containers

GPU support is enabled automatically for containers with NVIDIA GPUs, i.e. with `NVIDIA_VISIBLE_DEVICES` set, as in CUDA images. For other images, set it with `--env NVIDIA_VISIBLE_DEVICES=all`, or add `--annotation cedana.ai/gpu=true`. See [checkpoint/restore with GPUs](gpu.md) for GPU prerequisites.

1. Run:

```sh
sudo podman run -d --runtime cedana --gpus <gpus> --name <container> <cuda-image> [command] [args...]
```

2. Checkpoint:

```sh
cedana dump podman <container> --dir <dump-dir>
```

3. Restore:

```sh
sudo podman rm <container>
sudo podman run -d --runtime cedana --gpus <gpus> --annotation cedana.ai/checkpoint-path=<dump-dir>/<dump> --name <container> <cuda-image>
```

A checkpoint does not record the container's GPUs, so pass `--gpus` again on restore. The checkpointed GPU state is restored onto the given GPUs.

{% hint style="info" %}
Instead of podman's `--gpus` (podman 5.0+), GPUs can also be added with CDI, e.g. `--device nvidia.com/gpu=<N>`, for both run and restore.
{% endhint %}

## How it works

Checkpointing is delegated to the plugin of the container's OCI runtime, as a rootful podman container is just a regular **crun** (or **runc**) container.

Running and restoring however must go through podman, as podman owns the container's sandbox: its network namespace, rootfs mount, and the `conmon` process that monitors it. For this, the container is created with the Cedana OCI runtime (`cedana-oci-runtime`, from the **oci-runtime** plugin), which podman invokes in place of **crun**/**runc**. It routes the container creation to `cedana run <runtime>`, or `cedana restore <runtime>` for a restore, and passes every other command through to the real runtime. This is podman's equivalent of the Cedana containerd runtime (shim).

### Annotations

The Cedana OCI runtime reads the following container annotations. The podman plugin sets them, and they can also be set directly with `podman run --annotation <key>=<value>`. JSON values may be base64-encoded, as some podman versions split `--annotation` values on commas.

| Annotation                  | Description                                                                 |
| --------------------------- | --------------------------------------------------------------------------- |
| `cedana.ai/runtime`         | The real runtime (`crun` or `runc`). Default: `crun` if installed, else `runc` |
| `cedana.ai/checkpoint-path` | Restore from this checkpoint, instead of creating                           |
| `cedana.ai/criu-opts`       | CRIU options JSON for the restore                                           |
| `cedana.ai/config`          | Cedana [configuration](../get-started/configuration.md) JSON to run Cedana with |
| `cedana.ai/gpu`             | Enable GPU interception. Automatic if `NVIDIA_VISIBLE_DEVICES` is set        |
| `cedana.ai/gpu-tracing`     | Enable GPU tracing                                                          |
| `cedana.ai/gpu-id`          | GPU controller to use, if already attached                                  |
| `cedana.ai/executable-path` | Cedana binary to use. Set by the podman plugin (disables automatic GPU interception, which the plugin handles itself) |

## Supported runtimes

| Runtime | Required plugins               |
| ------- | ------------------------------ |
| crun    | `crun`, `oci-runtime`          |
| runc    | `runc`, `oci-runtime`          |

The runtime is detected per-container. A checkpoint records the container's runtime, and a restore recreates the container with the same one.

## Read/write layer (rootfs)

Checkpoints automatically include the container's read/write layer (rootfs) in the dump, if podman's storage driver is `overlay` (the default). Any changes made to the container's rootfs before checkpointing will be preserved and restored. For other storage drivers (e.g. `vfs`), the read/write layer is skipped gracefully.

{% hint style="info" %}
For all available CLI options, see [CLI reference](../references/cli/cedana.md). Directly interacting with daemon is also possible through gRPC, see [API reference](../references/api.md).
{% endhint %}
