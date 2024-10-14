## Checkpoint/Restore GPU runc containers
> [!WARNING]
> This feature is still experimental.

This doc explains the daemon's features that can be used to do a GPU checkpoint/restore of runc containers that are enabled with GPU devices.

Currently, support is available for NVIDIA GPUs only.

### Spawn a GPU container

To spawn a runc container with NVIDIA GPU support, NVIDIA ships a runc `prestart` hook that can be used to set up the GPU environment for the container. For instance, when spawning a docker container with `--runtime=nvidia` ([more info](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html#configuring-docker)), the `nvidia-container-runtime-hook` is added to the runc bundle's `config.json` file. 

To enable cedana C/R support, there are 4 additional requirements:
1. Add/Mount the cedana GPU shared library `libcedana-gpu.so` to the container. This binary can be obtained when you run the daemon with GPU support enabled, i.e., `sudo cedana daemon start --gpu-enabled`.
2. Set the `LD_PRELOAD=libcedana-gpu.so` environment variable, or prefix to `process.args` in the container's `config.json`.
3. Bind the container's port 50051 to the host.
4. Share the container's shared memory (`/dev/shm`) with the host. 

All these steps can only be done before spawning the container (by modifying the bundle's `config.json`). In future releases, we plan to add a runc hook that will automate these steps, or add an option to spawn a runc container from the daemon itself.

### Start managing the container

The daemon supports a subcommand called `cedana manage` that is used to tell the daemon to start managing a container/process. This adds the container as a 'managed job' with a unique job ID, allowing a user to use the job subcommands with just the job ID to checkpoint/restore the container. 

Use the following command to start managing a container, with GPU support enabled:

```bash
cedana manage runc --id <container-id> --gpu-enabled
```
Use `cedana ps` to view list of managed jobs.

### Checkpoint/Restore the container

Once managing, you can use the `cedana job` subcommands to checkpoint/restore the container. The checkpoint/restore will automatically include GPU, as the container is being managed with GPU support enabled.

To checkpoint the container:

```bash
cedana dump job <container-id> --dir <dump-dir>
```
See `cedana dump job --help` for more options.

To restore the container:

```bash
cedana restore job <container-id>
```
See `cedana restore job --help` for more options.
