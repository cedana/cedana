## cedana query

Query containers/processes

### Options

```
  -h, --help      help for query
  -i, --inspect   view details of first result
  -t, --tree      include entire process tree
```

### Options inherited from parent commands

```
      --address string          address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
      --config string           one-time config JSON string (merge with existing config)
      --config-dir string       custom config directory
      --init-config             initialize config file with defaults and env var overrides
      --merge-config            same as --init-config but does not overwrite existing config file, only merges new values into it
      --profiling               enable profiling/show profiling data
      --profiling-path string   path to write profiling JSON to (if enabled)
      --protocol string         protocol to use (TCP, UNIX, VSOCK)
```

### SEE ALSO

* [cedana](cedana.md)	 - Root command for Cedana
* [cedana query containerd](cedana_query_containerd.md)	 - Query containerd containers
* [cedana query crun](cedana_query_crun.md)	 - Query crun containers
* [cedana query k8s](cedana_query_k8s.md)	 - Query Kubernetes pods and containers
* [cedana query process](cedana_query_process.md)	 - Query a process
* [cedana query runc](cedana_query_runc.md)	 - Query runc containers

