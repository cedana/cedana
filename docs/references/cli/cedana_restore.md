## cedana restore

Restore a container/process

### Options

```
  -a, --attach             attach stdin/out/err
      --attachable         make it attachable, but don't attach
      --criu-opts string   CRIU options JSON (overriddes individual CRIU flags)
      --external strings   resources from external namespaces (can be multiple)
      --file-locks         restore file locks
  -h, --help               help for restore
      --leave-stopped      leave the process stopped after restore
      --link-remap         remap links to invisible files during restore
      --no-server          select how to run restores
  -o, --out string         log path to forward stdout/err
  -p, --path string        path of dump
      --pid-file string    file to write PID to
      --shell-job          process is not session leader (shell job)
      --tcp-close          allow listening TCP sockets to exist on restore
      --tcp-established    restore tcp established connections
```

### Options inherited from parent commands

```
      --address string      address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
      --config string       one-time config JSON string (merge with existing config)
      --config-dir string   custom config directory
      --profiling           enable profiling/show profiling data
      --protocol string     protocol to use (TCP, UNIX, VSOCK)
```

### SEE ALSO

* [cedana](cedana.md)	 - Root command for Cedana
* [cedana restore containerd](cedana_restore_containerd.md)	 - Restore a containerd container
* [cedana restore job](cedana_restore_job.md)	 - Restore a managed process/container (job)
* [cedana restore process](cedana_restore_process.md)	 - Restore a process
* [cedana restore runc](cedana_restore_runc.md)	 - Restore a runc container

