## cedana freeze

Freeze a container/process

### Options

```
      --gpu-freeze-type string   GPU freeze type (IPC, NCCL)
  -h, --help                     help for freeze
```

### Options inherited from parent commands

```
      --address string      address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
      --config string       one-time config JSON string (merge with existing config)
      --config-dir string   custom config directory
      --profiling           enable profiling/show profiling data
      --protocol string     protocol to use (TCP, UNIX, VSOCK)
```

### SEE ALSO

* [cedana](cedana.md)	 - Root command for Cedana
* [cedana freeze containerd](cedana_freeze_containerd.md)	 - Freeze a containerd container
* [cedana freeze job](cedana_freeze_job.md)	 - Freeze a managed process/container (job)
* [cedana freeze process](cedana_freeze_process.md)	 - Freeze a process
* [cedana freeze runc](cedana_freeze_runc.md)	 - Freeze a runc container

