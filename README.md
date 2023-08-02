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

To use Cedana in a standalone context, you can directly checkpoint and restore processes with the cedana client. 

### Checkpointing 

```sh
sudo cedana client dump -p PID -d DIR 
```
Cedana needs `sudo` to properly walk through `/proc/pid`. Running `dump` creates a `process_name_datetime.zip` file in the directory specified with `-d`. Alternatively, you can forego the flag by describing a folder to store the checkpoint by modifying the config at `~/.cedana/cedana_config.json`: 

```json 
"shared_storage": {
    "dump_storage_dir": "/home/johnAdams/cedana_dumps/"
  }
```

See the configuration section for more toggles. 

### Restoring 

```sh 
sudo cedana client restore /path/to/zip
```


## Gotchas
If running locally, you need to wrap your process with `setsid` and redirect the output. So if you're running (and then trying to checkpoint) a jupyter notebook, you would run: 
```sh
setsid jupyter notebook --port 8000 < /dev/null &> output.log & 
```
which redirects `stdin` to `/dev/null` and `stdout & stderr` to `output.log`. 


## Configuration 

### Signals 
If you'd like your process to do some work prior to being checkpointed (saving some other state (e.g create a pyTorch checkpoint), closing network connections, etc.) you can configure `cedana` to send the process a `SIGUSR1` signal and wait for a prescribed amount of time: 

```json
  "client": {
    "signal_process_pre_dump": true,
    "signal_process_timeout": 30
  }
```

the files created get bundled up with the rest of the checkpoint into the zip. 

### Process control 
You can also choose to leave the process running after checkpointing. This defaults to true (for minimal invasiveness) if your configuration is generated for the first time by `cedana`. Additionally, you can also choose to set a process name if you'd rather not do PID discovery every time prior to checkpointing: 

```json 
"client": {
    "process_name": "jupyter notebook",
    "leave_running": false,
  }
```
We then just calculate the [Levenshtein distance](https://en.wikipedia.org/wiki/Levenshtein_distance) between the provided string and running process names to find the PID. 

### Storage
As mentioned previously, you can configure where the checkpoints are stored with: 
```json
"shared_storage": {
    "dump_storage_dir": "/home/johnAdams/cedana_dumps/",
    "mount_point": "/mnt/shared_storage",
  }
```
`mount_point` is used if you'd like to move a created checkpoint to some other folder after it's been dumped; which is useful if you're using `cedana` to migrate between machines and you're using network attached storage (like EFS).

## Contributing
See CONTRIBUTING.md for guidelines. 