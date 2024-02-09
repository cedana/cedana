# Cedana

Build systems that bake realtime adaptiveness and elasticity using Cedana.

Cedana-client serves as client code to the larger Cedana system. We leverages CRIU to provide checkpoint and restore functionality for most linux processes (including docker containers).

We can monitor, migrate and automate checkpoints across a realtime network and compute configuration enabling ephemeral and hardware agnostic compute. See [our website](https://cedana.ai) for more information about our managed product.

Some problems Cedana can help solve include:

- Cold-starts for containers/processes
- Keeping a process running independent of hardware/network failure
- Managing multiprocess/multinode systems

You can get started using cedana today (outside of the base checkpoint/restore functionality) by trying out our [CLI tool](https://github.com/cedana/cedana-cli) that leverages this system to arbitrage compute across clouds.


## Build 

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
cedana client dump job JOBID -d DIR 
```
A successsful `dump` creates a `process_name_datetime.tar` file in the directory specified with `-d`. Alternatively, you can forego the flag by describing a folder to store the checkpoint in in the config: 

```json 
"shared_storage": {
    "dump_storage_dir": "/home/johnAdams/cedana_dumps/"
  }
```

See the configuration section for more toggles. 

### Restoring 

```sh 
cedana client restore job JOBID
```

Currently, we also support `runc` and by extension, `containerd` checkpointing, with more container runtime support planned in the future. Checkpointing these is as simple as prepending the `dump/restore` commands with the correct runtime. For example, to checkpoint a `containerd` container: 

```sh 
sudo cedana dump containerd -i test -p test 
```

where `i` is the imageRef and `p` is the containerID. 

## Gotchas
If running just a PID, you need to wrap your process with `setsid` and redirect the output. So if you're running (and then trying to checkpoint) a jupyter notebook, you would run: 
```sh
setsid jupyter notebook --port 8000 < /dev/null &> output.log & 
```
which redirects `stdin` to `/dev/null` and `stdout & stderr` to `output.log`. 

Alternatively, you can execute a task from `cedana` itself, using: 

```cedana start jupyter notebook --port 8000```

which handles the redirects and sets the process as a session leader. 

## Contributing
See CONTRIBUTING.md for guidelines. 