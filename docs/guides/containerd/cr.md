# Checkpoint/restore containerd

## Prerequisites

1. Create an account with Cedana, to get access to the containerd plugin. See [authentication](../../get-started/authentication.md).
2. Set the Cedana URL & authentication token in the [configuration](../../get-started/configuration.md).
3. Install the **containerd** plugin with `sudo cedana plugin install containerd`.
4. Install the containerd runtime plugin for the underlying OCI runtime (see [Supported runtimes](#supported-runtimes)):
   - If runtime type is `io.containerd.runc.v2`, install the **containerd runtime** plugin (shim) with `sudo cedana plugin install containerd/runtime-runc`.
   - If the runtime binary is `runc` (default), install the lower-level **runc** plugin with `sudo cedana plugin install runc`.
   - If the runtime binary is `crun`, install the lower-level **crun** plugin with `sudo cedana plugin install crun`.
5. Ensure the daemon is running, see [installation](../../get-started/installation.md).
6. Do a health check to ensure the plugin is ready, see [health checks](../../get-started/health.md).

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

3. Restore:

```sh
cedana restore job --attach <job_id>
```

## Supported runtimes

The containerd plugin delegates the container checkpoint/restore to the plugin of the underlying OCI runtime. Additionally, running or restoring a container through containerd requires the Cedana containerd runtime (shim) plugin for that runtime type — e.g. **containerd/runtime-runc** for the `io.containerd.runc.v2` runtime type — which is used as the container's runtime in place of the stock shim.

The currently supported runtimes are:

| Runtime         | Runtime type                                                        | Required plugins                  |
| --------------- | ------------------------------------------------------------------- | --------------------------------- |
| runc            | `io.containerd.runc.v2`                                             | `runc`, `containerd/runtime-runc` |
| crun            | `io.containerd.runc.v2` (`BinaryName = "crun"`)                     | `crun`, `containerd/runtime-runc` |
| nvidia (legacy) | `io.containerd.runc.v2` (`BinaryName = "nvidia-container-runtime"`) | `runc`, `containerd/runtime-runc` |

Note that **crun** runs under the same shim as **runc**, so both runtimes share the **containerd/runtime-runc** plugin.

The **nvidia** runtime (`nvidia-container-runtime`) is a thin wrapper around **runc** — it injects NVIDIA's prestart hook into the container spec and then execs **runc** — so its containers are regular runc containers, handled by the **runc** plugin. This hook-based runtime is NVIDIA's legacy approach to GPU support: modern containerd injects GPU devices through [CDI](https://github.com/cncf-tags/container-device-interface) (supported natively since containerd 1.7, enabled by default in 2.0) without requiring a special runtime, and CDI is also what Cedana's own GPU interception uses. Containers using the nvidia runtime nevertheless continue to work.

The runtime is detected per-container from its runtime options. Since **crun** uses the **runc** runtime type with just a different runtime binary (the `BinaryName` runtime option), a containerd runtime configured like below is automatically routed to the **crun** plugin:

```toml
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.crun]
  runtime_type = "io.containerd.runc.v2"
  [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.crun.options]
    BinaryName = "crun"
```

This works regardless of whether **crun** is the default runtime (`default_runtime_name = "crun"`) or selected per-container (e.g. `ctr run --runc-binary crun`). A checkpoint records the container's runtime and runtime binary, and a restore recreates the container with the same ones.

## Read/write layer (rootfs)

Checkpoints automatically include the container's read/write layer (rootfs) in the dump. Any changes made to the container's rootfs before checkpointing will be preserved and restored.

Whether the read/write layer can be captured depends on the filesystem of the container's rootfs mount, which under containerd is determined by the [snapshotter](https://github.com/containerd/containerd/tree/main/docs/snapshotters) in use:

| Snapshotter                               | Rootfs filesystem | Read/write layer support |
| ----------------------------------------- | ----------------- | ------------------------ |
| `overlayfs` (default)                     | `overlay`         | ✔                        |
| `native`                                  | plain directory   | ✖                        |
| others (`btrfs`, `zfs`, `devmapper`, ...) | various           | ✖                        |

For unsupported snapshotters, the read/write layer is skipped gracefully — the checkpoint still succeeds, but changes made to the container's rootfs are not preserved across a restore.

{% hint style="info" %}
For all available CLI options, see [CLI reference](../../references/cli/cedana.md). Directly interacting with daemon is also possible through gRPC, see [API reference](../../references/api.md).
{% endhint %}
