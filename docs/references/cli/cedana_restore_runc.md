## cedana restore runc

Restore a runc container

```
cedana restore runc [flags]
```

### Options

```
  -b, --bundle string   bundle
  -h, --help            help for runc
  -i, --id string       new id
  -r, --root string     root
```

### Options inherited from parent commands

```
      --address string      address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
  -a, --attach              attach stdin/out/err
      --config string       one-time config JSON string (merge with existing config)
      --config-dir string   custom config directory
      --external strings    resources from external namespaces (can be multiple)
      --file-locks          restore file locks
      --leave-stopped       leave the process stopped after restore
      --link-remap          remap links to invisible files during restore
      --log string          log path to forward stdout/err
  -p, --path string         path of dump
      --profiling           enable profiling/show profiling data
      --protocol string     protocol to use (TCP, UNIX, VSOCK)
      --shell-job           process is not session leader (shell job)
      --stream int32        stream the restore (using <n> parallel streams)
      --tcp-close           allow listening TCP sockets to exist on restore
      --tcp-established     restore tcp established connections
```

### SEE ALSO

* [cedana restore](cedana_restore.md)	 - Restore a container/process

###### Auto generated by spf13/cobra on 1-Apr-2025
