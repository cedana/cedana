## cedana restore crun

Restore a crun container

```
cedana restore crun [flags]
```

### Options

```
  -b, --bundle string                 bundle
      --console-socket string         path to an AF_UNIX socket which will receive a file descriptor referencing the master end of the console's pseudoterminal
  -d, --detach                        detach from the container's process, ignored if not using --no-server and is always true
  -h, --help                          help for crun
      --id string                     new id
      --log string                    log file to write logs to if --no-server
      --log-format string             log format to use if --no-server (json, text) (default "text")
      --netns-eth0-ipv4-addr string   new IPv4 address for eth0 in the restored container
      --no-new-keyring                do not create a new session keyring.
      --no-pivot                      do not use pivot root to jail process inside rootfs.
      --no-subreaper                  disable the use of the subreaper used to reap reparented processes
      --root string                   root
      --rootless string               ignore cgroup permission errors (true, false, auto) (default "auto")
      --systemd-cgroup                enable systemd cgroup support, expects cgroupsPath to be of form 'slice:prefix:name' for e.g. 'system.slice:runc:434234'
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

