## cedana freeze containerd

Freeze a containerd container

### Synopsis

Freeze a containerd container

```
cedana freeze containerd <container-id> [flags]
```

### Options

```
      --address string     containerd socket address
  -h, --help               help for containerd
      --namespace string   containerd namespace
```

### Options inherited from parent commands

```
      --config string            one-time config JSON string (merge with existing config)
      --config-dir string        custom config directory
      --gpu-freeze-type string   GPU freeze type (IPC, NCCL)
      --profiling                enable profiling/show profiling data
      --protocol string          protocol to use (TCP, UNIX, VSOCK)
```

### SEE ALSO

* [cedana freeze](cedana_freeze.md)	 - Freeze a container/process

