## cedana dump job

Dump a managed process/container (job)

```
cedana dump job <JID> [flags]
```

### Options

```
      --address string     (containerd) containerd socket address
  -h, --help               help for job
      --image string       (containerd) image ref (rootfs). leave empty to skip rootfs
      --namespace string   (containerd) containerd namespace
      --root string        (runc) root
      --rootfs             (containerd) dump with rootfs
      --rootfs-only        (containerd) dump only the rootfs
```

### Options inherited from parent commands

```
      --compression string      compression algorithm (none, tar, gzip, lz4, zlib)
      --config string           one-time config JSON string (merge with existing config)
      --config-dir string       custom config directory
      --criu-opts string        criu options JSON (overriddes individual CRIU flags)
  -d, --dir string              directory to dump into
      --external strings        resources from external namespaces (can be multiple)
      --file-locks              dump file locks
      --init-config             initialize config file with defaults and env var overrides
      --leave-running           leave the process running after dump
      --link-remap              remap links to files in the dump
      --merge-config            same as --init-config but does not overwrite existing config file, only merges new values into it
      --name string             name of the dump
      --no-server               run without server
      --profiling               enable profiling/show profiling data
      --profiling-path string   path to write profiling JSON to (if enabled)
      --protocol string         protocol to use (TCP, UNIX, VSOCK)
      --result-file string      write the dump result (DumpResp) as JSON to this file
      --shell-job               process is not session leader (shell job)
      --skip-in-flight          skip in-flight tcp connections
      --streams int32           number of streams to use for dump (0 for no streaming)
      --tcp-established         dump tcp established connections
```

### SEE ALSO

* [cedana dump](cedana_dump.md)	 - Dump a container/process

