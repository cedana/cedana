## cedana run containerd

Run a containerd container

```
cedana run containerd <image|rootfs> [command] [args...] [flags]
```

### Options

```
      --address string     containerd socket address
      --gpus int32Slice    add GPUs to the container (e.g. 0,1,2) (default [])
  -h, --help               help for containerd
      --id string          containerd id
      --namespace string   containerd namespace
      --no-pivot           disable use of pivot-root
```

### Options inherited from parent commands

```
  -a, --attach              attach stdin/out/err
      --attachable          make it attachable, but don't attach
      --config string       one-time config JSON string (merge with existing config)
      --config-dir string   custom config directory
  -g, --gpu-enabled         enable GPU support
      --gpu-tracing         enable GPU tracing
  -j, --jid string          job id
      --no-server           run without server
  -o, --out string          file to forward stdout/err
      --pid-file string     file to write PID to
      --profiling           enable profiling/show profiling data
      --protocol string     protocol to use (TCP, UNIX, VSOCK)
```

### SEE ALSO

* [cedana run](cedana_run.md)	 - Run a managed process/container (create a job)

