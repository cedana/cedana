## cedana run

Run a managed process/container (create a job)

### Options

```
  -a, --attach            attach stdin/out/err
      --attachable        make it attachable, but don't attach
  -g, --gpu-enabled       enable GPU support
      --gpu-id string     specify existing GPU controller ID to attach (internal use only)
      --gpu-tracing       enable GPU tracing
  -h, --help              help for run
  -j, --jid string        job id
      --no-server         run without server
  -o, --out string        file to forward stdout/err
      --pid-file string   file to write PID to
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
* [cedana run containerd](cedana_run_containerd.md)	 - Run a containerd container
* [cedana run crun](cedana_run_crun.md)	 - run a crun container
* [cedana run process](cedana_run_process.md)	 - Run a managed process (job)
* [cedana run runc](cedana_run_runc.md)	 - run a runc container

