## Checkpoint/restore with GPUs

### Prerequisites
1. Create an account with Cedana, to get access to the GPU plugin. See [authentication](../authentication.md).
2. Set the Cedana URL & authentication token in the [configuration](../configuration.md).
3. Install the GPU plugin with `sudo cedana plugin install gpu`.
4. Ensure the daemon is running, see [installation](../installation.md).
5. Do a health check to ensure the plugin is ready, see [health checks](../health.md).

### Usage
**NOTE**: GPU checkpoint/restore is only possible for managed processes/containers, i.e., those that are spawned using `cedana run` (see [managed process/container](../managed.md)).

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

For all available CLI options, see [CLI reference](../cli/cedana.md). Directly interacting with daemon is also possible through gRPC, see [API reference](../api.md).
