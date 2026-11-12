## cedana manage containerd

Manage a containerd container

```
cedana manage containerd <container-id> [flags]
```

### Options

```
      --address string     containerd socket address
  -h, --help               help for containerd
      --image string       image to use
      --namespace string   containerd namespace
```

### Options inherited from parent commands

```
      --config string           one-time config JSON string (merge with existing config)
      --config-dir string       custom config directory
  -g, --gpu-enabled             enable GPU support
      --gpu-id string           specify existing GPU controller ID to attach (internal use only)
      --gpu-tracing             enable GPU tracing
      --init-config             initialize config file with defaults and env var overrides
  -j, --jid string              job id
      --merge-config            same as --init-config but does not overwrite existing config file, only merges new values into it
      --pid-file string         file to write PID to
      --profiling               enable profiling/show profiling data
      --profiling-path string   path to write profiling JSON to (if enabled)
      --protocol string         protocol to use (TCP, UNIX, VSOCK)
      --upcoming                wait for upcoming process/container
```

### SEE ALSO

* [cedana manage](cedana_manage.md)	 - Manage an existing/upcoming process/container (create a job)

