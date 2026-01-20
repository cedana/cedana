## cedana manage runc

manage an existing runc container

```
cedana manage runc <container-id> [flags]
```

### Options

```
  -b, --bundle string   bundle
  -h, --help            help for runc
      --root string     root
```

### Options inherited from parent commands

```
      --address string      address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
      --config string       one-time config JSON string (merge with existing config)
      --config-dir string   custom config directory
  -g, --gpu-enabled         enable GPU support
      --gpu-id string       specify existing GPU controller ID to attach (internal use only)
      --gpu-tracing         enable GPU tracing
  -j, --jid string          job id
      --pid-file string     file to write PID to
      --profiling           enable profiling/show profiling data
      --protocol string     protocol to use (TCP, UNIX, VSOCK)
      --upcoming            wait for upcoming process/container
```

### SEE ALSO

* [cedana manage](cedana_manage.md)	 - Manage an existing/upcoming process/container (create a job)

