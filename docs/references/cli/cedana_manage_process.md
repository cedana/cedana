## cedana manage process

Managed existing process (job)

```
cedana manage process <PID> [args...] [flags]
```

### Options

```
  -h, --help   help for process
```

### Options inherited from parent commands

```
      --address string      address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
      --config string       one-time config JSON string (merge with existing config)
      --config-dir string   custom config directory
  -g, --gpu-enabled         enable GPU support
  -j, --jid string          job id
      --profiling           enable profiling/show profiling data
      --protocol string     protocol to use (TCP, UNIX, VSOCK)
      --upcoming            wait for upcoming process/container
```

### SEE ALSO

* [cedana manage](cedana_manage.md)	 - Manage an existing/upcoming process/container (create a job)

