## cedana restore job

Restore a managed process/container (job)

```
cedana restore job <JID> [flags]
```

### Options

```
      --address string          (containerd) containerd socket address
  -b, --bundle string           (runc) bundle
      --console-socket string   (runc) path to an AF_UNIX socket which will receive a file descriptor referencing the master end of the console's pseudoterminal
  -d, --detach                  (runc) detach from the container's process, ignored if not using --no-server and is always true
      --gpus int32Slice         (containerd) add GPUs to the container (e.g. 0,1,2) (default [])
  -h, --help                    help for job
      --id string               (runc) new id
      --image string            (containerd) image to use
      --log string              (runc) log file to write logs to if --no-server
      --log-format string       (runc) log format to use if --no-server (json, text) (default "text")
      --namespace string        (containerd) containerd namespace
      --no-new-keyring          (runc) do not create a new session keyring.
      --no-pivot                (runc) do not use pivot root to jail process inside rootfs.
      --no-subreaper            (runc) disable the use of the subreaper used to reap reparented processes
      --root string             (runc) root
      --rootless string         (runc) ignore cgroup permission errors (true, false, auto) (default "auto")
      --systemd-cgroup          (runc) enable systemd cgroup support, expects cgroupsPath to be of form 'slice:prefix:name' for e.g. 'system.slice:runc:434234'
```

### Options inherited from parent commands

```
  -a, --attach              attach stdin/out/err
      --attachable          make it attachable, but don't attach
      --config string       one-time config JSON string (merge with existing config)
      --config-dir string   custom config directory
      --criu-opts string    CRIU options JSON (overriddes individual CRIU flags)
      --external strings    resources from external namespaces (can be multiple)
      --file-locks          restore file locks
      --leave-stopped       leave the process stopped after restore
      --link-remap          remap links to invisible files during restore
      --no-server           select how to run restores
  -o, --out string          log path to forward stdout/err
  -p, --path string         path of dump
      --pid-file string     file to write PID to
      --profiling           enable profiling/show profiling data
      --protocol string     protocol to use (TCP, UNIX, VSOCK)
      --shell-job           process is not session leader (shell job)
      --tcp-close           allow listening TCP sockets to exist on restore
      --tcp-established     restore tcp established connections
```

### SEE ALSO

* [cedana restore](cedana_restore.md)	 - Restore a container/process

