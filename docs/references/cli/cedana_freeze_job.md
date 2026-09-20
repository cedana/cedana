## cedana freeze job

Freeze a managed process/container (job)

```
cedana freeze job <JID> [flags]
```

### Options

```
      --address string     (containerd) containerd socket address
  -h, --help               help for job
      --namespace string   (containerd) containerd namespace
      --root string        (runc) root
```

### Options inherited from parent commands

```
      --config string           one-time config JSON string (merge with existing config)
      --config-dir string       custom config directory
      --init-config             initialize config file with defaults and env var overrides
      --merge-config            same as --init-config but does not overwrite existing config file, only merges new values into it
      --profiling               enable profiling/show profiling data
      --profiling-path string   path to write profiling JSON to (if enabled)
      --protocol string         protocol to use (TCP, UNIX, VSOCK)
```

### SEE ALSO

* [cedana freeze](cedana_freeze.md)	 - Freeze a container/process

