## cedana slurm

Helper for setting up and running in Slurm

### Options

```
  -h, --help   help for slurm
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
* [cedana slurm destroy](cedana_slurm_destroy.md)	 - Destroy and cleanup cedana on host
* [cedana slurm setup](cedana_slurm_setup.md)	 - Setup and start cedana on host

