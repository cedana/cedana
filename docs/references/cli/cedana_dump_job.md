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
      --compression string       compression algorithm (none, tar, gzip, lz4, zlib)
      --config string            one-time config JSON string (merge with existing config)
      --config-dir string        custom config directory
      --criu-opts string         criu options JSON (overriddes individual CRIU flags)
  -d, --dir string               directory to dump into
      --external strings         resources from external namespaces (can be multiple)
      --file-locks               dump file locks
      --gpu-freeze-type string   GPU freeze type (IPC, NCCL)
      --leave-running            leave the process running after dump
      --link-remap               remap links to files in the dump
      --name string              name of the dump
      --profiling                enable profiling/show profiling data
      --protocol string          protocol to use (TCP, UNIX, VSOCK)
      --shell-job                process is not session leader (shell job)
      --skip-in-flight           skip in-flight tcp connections
      --streams int32            number of streams to use for dump (0 for no streaming)
      --tcp-established          dump tcp established connections
```

### SEE ALSO

* [cedana dump](cedana_dump.md)	 - Dump a container/process

