## cedana dump process

Dump a process

```
cedana dump process <PID> [flags]
```

### Options

```
  -h, --help   help for process
```

### Options inherited from parent commands

```
      --address string       address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)
      --compression string   compression algorithm (none, tar, gzip, lz4, zlib)
      --config string        one-time config JSON string (merge with existing config)
      --config-dir string    custom config directory
      --criu-opts string     criu options JSON (overriddes individual CRIU flags)
  -d, --dir string           directory to dump into
      --external strings     resources from external namespaces (can be multiple)
      --file-locks           dump file locks
      --leave-running        leave the process running after dump
      --link-remap           remap links to files in the dump
      --name string          name of the dump
      --profiling            enable profiling/show profiling data
      --protocol string      protocol to use (TCP, UNIX, VSOCK)
      --shell-job            process is not session leader (shell job)
      --skip-in-flight       skip in-flight tcp connections
      --streams int32        number of streams to use for dump (0 for no streaming)
      --tcp-established      dump tcp established connections
```

### SEE ALSO

* [cedana dump](cedana_dump.md)	 - Dump a container/process

