## cedana run process

Run a managed process (job)

```
cedana run process <path> [args...] [flags]
```

### Options

```
      --as-root   run as root
  -h, --help      help for process
```

### Options inherited from parent commands

```
      --address string          address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
  -a, --attach                  attach stdin/out/err
      --attachable              make it attachable, but don't attach
      --config string           one-time config JSON string (merge with existing config)
      --config-dir string       custom config directory
  -g, --gpu-enabled             enable GPU support
      --gpu-id string           specify existing GPU controller ID to attach (internal use only)
      --gpu-tracing             enable GPU tracing
      --init-config             initialize config file with defaults and env var overrides
  -j, --jid string              job id
      --merge-config            same as --init-config but does not overwrite existing config file, only merges new values into it
      --no-server               run without server
  -o, --out string              file to forward stdout/err
      --pid-file string         file to write PID to
      --profiling               enable profiling/show profiling data
      --profiling-path string   path to write profiling JSON to (if enabled)
      --protocol string         protocol to use (TCP, UNIX, VSOCK)
```

### SEE ALSO

* [cedana run](cedana_run.md)	 - Run a managed process/container (create a job)

