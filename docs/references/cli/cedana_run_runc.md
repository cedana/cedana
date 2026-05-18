## cedana run runc

run a runc container

```
cedana run runc [optional-id] [flags]
```

### Options

```
  -b, --bundle string           bundle
      --console-socket string   path to an AF_UNIX socket which will receive a file descriptor referencing the master end of the console's pseudoterminal
  -d, --detach                  detach from the container's process, ignored if not using --no-server and is always true
  -h, --help                    help for runc
      --log string              log file to write logs to if --no-server
      --log-format string       log format to use if --no-server (json, text) (default "text")
      --no-new-keyring          do not create a new session keyring.
      --no-pivot                do not use pivot root to jail process inside rootfs.
      --no-subreaper            disable the use of the subreaper used to reap reparented processes
      --preserve-fds int32      pass N additional file descriptors to the container (stdio + $LISTEN_FDS + N in total)
      --root string             root
      --rootless string         ignore cgroup permission errors (true, false, auto) (default "auto")
      --systemd-cgroup          enable systemd cgroup support, expects cgroupsPath to be of form 'slice:prefix:name' for e.g. 'system.slice:runc:434234'
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

* [cedana run](cedana_run.md)	 - Run a managed process/container (create a job)

