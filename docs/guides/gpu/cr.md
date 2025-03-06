# Checkpoint/restore with GPUs

## Prerequisites

1. Create an account with Cedana, to get access to the GPU plugin. See [authentication](../../get-started/authentication.md).
2. Set the Cedana URL & authentication token in the [configuration](../../get-started/configuration.md).
3. Install a GPU plugin:

  - **Option 1: GPU Plugin**

    The GPU plugin is Cedana's proprietary plugin for high performance GPU checkpoint/restore. If unavailable to you, check option 2.
    - _Minimum NVIDIA driver version: 452 (API 11.8)_
    - _Maximum NVIDIA driver version: 550 (API 12.4). Newer drivers are unstable and may not work._
    - _Minimum CRIU version: 3.0_

    ```sh
    sudo cedana plugin install gpu
    ```
  - **Option 2: CRIU CUDA Plugin**
    - _Minimum NVIDIA driver version: 570 (API 12.8)_
    - _Minimum CRIU version: 4.0. Or, simply [install CRIU plugin](../../get-started/installation.md#install-criu)._

    ```sh
    sudo cedana plugin install criu/cuda
    ```
4. Ensure the daemon is running, see [installation](../../get-started/installation.md).
5. Do a health check to ensure the plugin is ready, see [health checks](../../get-started/health.md).

## Usage (GPU plugin)

**NOTE**: Cedana GPU checkpoint/restore is only possible for managed processes/containers, i.e., those that are spawned using `cedana run --gpu-enabled` (see [managed process/container](../managed.md)).

1. You may clone the [cedana-samples repository](https://github.com/cedana/cedana-samples) for some example GPU workloads.
2. Run a process with GPU support:

```sh
cedana run process --attach --gpu-enabled --jid <job_id> -- cedana-samples/gpu_smr/vector_add
```

3. Checkpoint:

```sh
cedana dump job <job_id>
```

4. Restore:

```sh
cedana restore job --attach <job_id>
```

## Usage (CRIU CUDA plugin)

You can checkpoint/restore normally as you do for CPU workloads. See [checkpoint/restore basics](../cr.md).

For all available CLI options, see [CLI reference](../../references/cli/cedana.md). Directly interacting with daemon is also possible through gRPC, see [API reference](../../references/api.md).
