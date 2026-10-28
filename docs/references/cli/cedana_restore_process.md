## cedana restore process

Restore a process

```
cedana restore process [flags]
```

### Options

```
  -h, --help   help for process
```

### Options inherited from parent commands

```
      --address string          address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
  -a, --attach                  attach stdin/out/err
      --attachable              make it attachable, but don't attach
      --config string           one-time config JSON string (merge with existing config)
      --config-dir string       custom config directory
      --criu-opts string        CRIU options JSON (overriddes individual CRIU flags)
      --external strings        resources from external namespaces (can be multiple)
      --file-locks              restore file locks
      --gpu-id string           specify existing GPU controller ID to attach (internal use only)
      --init-config             initialize config file with defaults and env var overrides
      --leave-stopped           leave the process stopped after restore
      --link-remap              remap links to invisible files during restore
      --merge-config            same as --init-config but does not overwrite existing config file, only merges new values into it
      --no-server               run without server
  -o, --out string              log path to forward stdout/err
  -p, --path string             path of dump
      --pid-file string         file to write PID to
      --profiling               enable profiling/show profiling data
      --profiling-path string   path to write profiling JSON to (if enabled)
      --protocol string         protocol to use (TCP, UNIX, VSOCK)
      --shell-job               process is not session leader (shell job)
      --tcp-close               allow listening TCP sockets to exist on restore
      --tcp-established         restore tcp established connections
```

### SEE ALSO

* [cedana restore](cedana_restore.md)	 - Restore a container/process

