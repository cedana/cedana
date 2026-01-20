## cedana query process

Query a process

```
cedana query process <PID1> [<PID2> ...] [flags]
```

### Options

```
  -h, --help   help for process
```

### Options inherited from parent commands

```
      --address string      address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
      --config string       one-time config JSON string (merge with existing config)
      --config-dir string   custom config directory
  -i, --inspect             view details of first result
      --profiling           enable profiling/show profiling data
      --protocol string     protocol to use (TCP, UNIX, VSOCK)
  -t, --tree                include entire process tree
```

### SEE ALSO

* [cedana query](cedana_query.md)	 - Query containers/processes

