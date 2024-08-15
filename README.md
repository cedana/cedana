# Cedana
![GitHub Release](https://img.shields.io/github/v/release/cedana/cedana) ![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/cedana/cedana/release.yml?branch=main)

Build systems that bake real-time adaptiveness and elasticity using Cedana.

This serves as client code to the larger Cedana system. We leverage [CRIU](https://github.com/checkpoint-restore/criu) to provide checkpoint and restore functionality for most linux processes (including containers).

We can monitor, migrate and automate checkpoints across a real-time network and compute configuration enabling ephemeral and hardware agnostic compute. See [our website](https://cedana.ai) for more information about our managed product.

Some problems Cedana can help solve include:

- Cold-starts for containers/processes
- Keeping a process running independent of hardware/network failure
- Managing multiprocess/multinode systems

You can get started using cedana today (outside of the base checkpoint/restore functionality) by trying out our [CLI tool](https://github.com/cedana/cedana-cli) that leverages this system to arbitrage compute across clouds.


## Build

Cedana needs `libgpgme`, `libbtrfs` and `libseccomp` on the machine to build against. On a debian based system, you can install them with: 

``` sh
apt install libgpgme-dev libseccomp-dev libbtrfs-dev
```

on centOS/RHEL: 

``` sh
yum install gpgme-devel libseccomp-devel btrfs-progs-devel 
```

To build: 

```go build```


## Usage

To use Cedana in a standalone context, you can directly checkpoint and restore processes with the cedana client. Configuration gets created at `~/.cedana/cedana_config.json` by calling `cedana bootstrap`. To use Cedana, you'll need to spin up the daemon, which is a simple gRPC daemon listening on 8080:

```sh
sudo cedana daemon start
```

All further commands interact with the daemon over RPC.


## Launching Work

Using cedana, you can checkpoint PIDs already running on the system, but may run into issues around process groups and/or file descriptors and network sockets. To bridge this gap and make the jobs more migratable, you can launch processes or work using `cedana exec`. For example:

```sh
cedana exec 'python3 example.py' example_job
```

where example_job is a job id associated with your task. To see tasks managed by `cedana`, you can use:

```sh
cedana ps
```

which also provides information about any local or remote checkpoints associated with the id. There's additional arguments you can pass to `exec` (such as passing a file for environment variables to launch the process with) which you can explore with `--help`.

### Checkpointing
To checkpoint a running job, you can run:

```sh
cedana dump job JOBID -d DIR
```
A successful `dump` creates a `process_name_datetime.tar` file in the directory specified with `-d`. Alternatively, you can forego the flag by describing a folder to store the checkpoint in in the config:

```json
"shared_storage": {
    "dump_storage_dir": "/home/johnAdams/cedana_dumps/"
  }
```

See the configuration section for more toggles.

### Restoring

```sh
cedana restore job JOBID
```

Currently, we also support `runc` and by extension Docker, `containerd` checkpointing and more container runtime support planned in the future. It should be noted that container checkpointing is generally orchestrated externally, leading the CLI options to be a little janky.

Checkpointing these is as simple as prepending the `dump/restore` commands with the correct runtime. For example, to checkpoint a `containerd` container:

```sh
cedana dump containerd -i test -p test
```

where `i` is the imageRef and `p` is the containerID.

For a Docker container (which generally wraps a runc runtime):

```sh
cedana dump runc -i runcID -d DIRECTORY
```

where `runcID` is the ID of the runc container (separate from what Docker daemon uses) which you can grab from `runc ps`. To restore, you'll need the container bundle, which you can pass to restore with `--bundle`. You can make a copy from a running container using `docker export CONTAINER_ID -o container_bundle.tar` and then:

```sh
cedana restore --bundle container_bundle.tar -i new_runc_id -d DIRECTORY
```

## Contributing
See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.
