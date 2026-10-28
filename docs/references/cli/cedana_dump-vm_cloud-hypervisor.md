## cedana dump-vm cloud-hypervisor

Dump a clh vm

### Synopsis

Dump a cloud-hypervisor virtual machine

```
cedana dump-vm cloud-hypervisor <vm-id> [flags]
```

### Options

```
  -h, --help   help for cloud-hypervisor
```

### Options inherited from parent commands

```
      --address string          address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
      --config string           one-time config JSON string (merge with existing config)
      --config-dir string       custom config directory
  -d, --dir string              directory to dump into
      --init-config             initialize config file with defaults and env var overrides
      --merge-config            same as --init-config but does not overwrite existing config file, only merges new values into it
      --profiling               enable profiling/show profiling data
      --profiling-path string   path to write profiling JSON to (if enabled)
      --protocol string         protocol to use (TCP, UNIX, VSOCK)
```

### SEE ALSO

* [cedana dump-vm](cedana_dump-vm.md)	 - Dump a VM

