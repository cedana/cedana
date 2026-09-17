## cedana unfreeze

Unfreeze a container/process

### Options

```
  -h, --help   help for unfreeze
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
* [cedana unfreeze containerd](cedana_unfreeze_containerd.md)	 - Unfreeze a containerd container
* [cedana unfreeze crun](cedana_unfreeze_crun.md)	 - Unfreeze a crun container
* [cedana unfreeze job](cedana_unfreeze_job.md)	 - Unfreeze a managed process/container (job)
* [cedana unfreeze process](cedana_unfreeze_process.md)	 - Unfreeze a process
* [cedana unfreeze runc](cedana_unfreeze_runc.md)	 - Unfreeze a runc container

