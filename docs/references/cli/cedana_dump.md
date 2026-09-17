## cedana dump

Dump a container/process

### Options

```
      --compression string   compression algorithm (none, tar, gzip, lz4, zlib)
      --criu-opts string     criu options JSON (overriddes individual CRIU flags)
  -d, --dir string           directory to dump into
      --external strings     resources from external namespaces (can be multiple)
      --file-locks           dump file locks
  -h, --help                 help for dump
      --leave-running        leave the process running after dump
      --link-remap           remap links to files in the dump
      --name string          name of the dump
      --no-server            run without server
      --result-file string   write the dump result (DumpResp) as JSON to this file
      --shell-job            process is not session leader (shell job)
      --skip-in-flight       skip in-flight tcp connections
      --streams int32        number of streams to use for dump (0 for no streaming)
      --tcp-established      dump tcp established connections
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
* [cedana dump containerd](cedana_dump_containerd.md)	 - Dump a containerd container
* [cedana dump crun](cedana_dump_crun.md)	 - Dump a crun container
* [cedana dump job](cedana_dump_job.md)	 - Dump a managed process/container (job)
* [cedana dump process](cedana_dump_process.md)	 - Dump a process
* [cedana dump runc](cedana_dump_runc.md)	 - Dump a runc container

