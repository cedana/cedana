## cedana query containerd

Query containerd containers

```
cedana query containerd <ID1> [<ID2> ...] [flags]
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
  -i, --inspect                 view details of first result
      --merge-config            same as --init-config but does not overwrite existing config file, only merges new values into it
      --profiling               enable profiling/show profiling data
      --profiling-path string   path to write profiling JSON to (if enabled)
      --protocol string         protocol to use (TCP, UNIX, VSOCK)
  -t, --tree                    include entire process tree
```

### SEE ALSO

* [cedana query](cedana_query.md)	 - Query containers/processes

