## cedana restore containerd

Restore a containerd container

```
cedana restore containerd [container-id] [args...] [flags]
```

### Options

```
      --address string     containerd socket address
      --gpus int32Slice    add GPUs to the container (e.g. 0,1,2) (default [])
  -h, --help               help for containerd
      --id string          new id
      --image string       image to use
      --namespace string   containerd namespace
      --no-pivot           disable use of pivot-root
```

### Options inherited from parent commands

```
  -a, --attach              attach stdin/out/err
      --attachable          make it attachable, but don't attach
      --config string       one-time config JSON string (merge with existing config)
      --config-dir string   custom config directory
      --criu-opts string    CRIU options JSON (overriddes individual CRIU flags)
      --external strings    resources from external namespaces (can be multiple)
      --file-locks          restore file locks
      --leave-stopped       leave the process stopped after restore
      --link-remap          remap links to invisible files during restore
      --no-server           select how to run restores
  -o, --out string          log path to forward stdout/err
  -p, --path string         path of dump
      --pid-file string     file to write PID to
      --profiling           enable profiling/show profiling data
      --protocol string     protocol to use (TCP, UNIX, VSOCK)
      --shell-job           process is not session leader (shell job)
      --tcp-close           allow listening TCP sockets to exist on restore
      --tcp-established     restore tcp established connections
```

### SEE ALSO

* [cedana restore](cedana_restore.md)	 - Restore a container/process

