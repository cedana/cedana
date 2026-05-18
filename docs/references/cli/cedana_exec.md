## cedana exec

Run a managed process (job) (alias of `run process <path> [args...]`)

### Synopsis

 (alias of `run process <path> [args...]`)

```
cedana exec <path> [args...] [flags]
```

### Options

```
      --as-root   run as root
  -h, --help      help for exec
```

### Options inherited from parent commands

```
      --address string      address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
  -a, --attach              attach stdin/out/err
      --attachable          make it attachable, but don't attach
      --config string       one-time config JSON string (merge with existing config)
      --config-dir string   custom config directory
  -g, --gpu-enabled         enable GPU support
      --gpu-id string       specify existing GPU controller ID to attach (internal use only)
      --gpu-tracing         enable GPU tracing
  -j, --jid string          job id
      --no-server           run without server
  -o, --out string          file to forward stdout/err
      --pid-file string     file to write PID to
      --profiling           enable profiling/show profiling data
      --protocol string     protocol to use (TCP, UNIX, VSOCK)
```

### SEE ALSO

* [cedana](cedana.md)	 - Root command for Cedana

