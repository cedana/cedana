## cedana unfreeze containerd

Unfreeze a containerd container

### Synopsis

Unfreeze a containerd container

```
cedana unfreeze containerd <container-id> [flags]
```

### Options

```
      --address string     containerd socket address
  -h, --help               help for containerd
      --namespace string   containerd namespace
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

* [cedana unfreeze](cedana_unfreeze.md)	 - Unfreeze a container/process

