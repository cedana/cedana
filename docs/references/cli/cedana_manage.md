## cedana manage

Manage an existing/upcoming process/container (create a job)

### Options

```
  -g, --gpu-enabled   enable GPU support
  -h, --help          help for manage
  -j, --jid string    job id
      --upcoming      wait for upcoming process/container
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
* [cedana manage containerd](cedana_manage_containerd.md)	 - Manage a containerd container
* [cedana manage process](cedana_manage_process.md)	 - Managed existing process (job)
* [cedana manage runc](cedana_manage_runc.md)	 - manage an existing runc container

