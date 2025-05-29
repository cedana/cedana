# Checkpoint/restore with GPUs

Checkpoint/restore with GPUs is currently only supported for NVIDIA GPUs.

## Prerequisites

1. Create an account with Cedana, to get access to the GPU plugin. See [authentication](../../get-started/authentication.md).
2. Set the Cedana URL & authentication token in the [configuration](../../get-started/configuration.md).
3. Install a GPU plugin.

*   **Option 1: GPU Plugin**

    ```sh
    sudo cedana plugin install gpu
    ```

    The GPU plugin is Cedana's proprietary plugin for high performance GPU checkpoint/restore that supports multi-process/node. If unavailable to you, check option 2.

*   **Option 2: CRIU CUDA Plugin**

    ```sh
    sudo cedana plugin install criu/cuda
    ```
    The CRIU CUDA plugin (CRIUgpu) is developed by the CRIU community and uses the [NVIDIA CUDA checkpoint utility](https://github.com/NVIDIA/cuda-checkpoint) under the hood.

4. Ensure the daemon is running, see [installation](../../get-started/installation.md).
5. Do a health check to ensure the plugin is ready, see [health checks](../../get-started/health.md).

Check out [Cedana vs. CRIU CUDA for GPU Checkpoint/Restore](https://app.gitbook.com/s/2VUqakyWqaX9NCnQNYjD/articles/cedana-vs.-criu-cuda-for-gpu-checkpoint-restore "mention") for a performance comparison between the two plugins.

|   | Min. NVIDIA Driver | Max. NVIDIA Driver | Multi-GPU | Multi-process/node | amd64 | arm64 |
|---|---------------------|---------------------|--------------------|-------|-------|-------|
| Cedana GPU | 452 (API 11.8) | 570 (API 12.8) | ✅ | ✅ | ✅ |✅ |
| CRIU CUDA | 570 (API 12.8) | 570 (API 12.8) | ❌ |❌ | ✅ | ❌ |

## Usage (GPU plugin)

**NOTE**: Cedana GPU checkpoint/restore is only possible for managed processes/containers, i.e., those that are spawned using `cedana run --gpu-enabled` or managed using `cedana manage --gpu-enabled` (see [managed process/container](../managed.md)).

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
