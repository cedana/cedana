## Health checks

Cedana can be health checked to ensure it fully supports the system and is ready to accept requests.

### Basic

To do a simple health check, ensure the daemon is running, and then run:

```sh
cedana daemon check
```

This just checks for basic checkpoint/restore capabilities on the host.

### Complete

To do a more complete health check, ensure the daemon is running, and then run:

```sh
cedana daemon check --full
```

This health checks all the installed plugins, including some optional checks. If you have installed plugins, you should see an output similar to:

```
CRIU
version                40100                                     ✔
features               available                                 ✔

GPU
status                 available                                 ✔
version                                                          ✔
device count           1                                         ✔
driver API             12040                                     ✔

STREAMER
version                v0.0.6                                    ✔
criu                   supported                                 ✔
pipe pages soft limit  16 KiB                                    •  For optimal performance, `echo 0 >
                                                                    /proc/sys/fs/pipe-user-pages-soft`
pipe pages hard limit  unlimited                                 ✔
pipe max size          1 MiB                                     •  For optimal performance, `echo 4194304 >
                                                                    /proc/sys/fs/pipe-max-size`

RUNC
version                v0.9.239-1-g9acef6f                       ✔
runc binary            available                                 ✔
runc version           1.2.4                                     ✔
runc spec              1.2.0                                     ✔
runc libseccomp        2.5.5                                     ✔

CONTAINERD
version                v0.9.239-1-g9acef6f                       ✔
containerd version     v2.0.1                                    ✔
containerd revision    88aa2f531d6c2922003cc7929e51daf1c14caa0a  ✔
containerd runtime     io.containerd.runc.v2                     ✔

Looks good, with 2 warning(s).

```

### Without daemon

You can also run the health check directly, without the daemon:

```sh
cedana check --full
```

Warnings are shown in yellow and are usually related to system configuration or performance. They are not critical but may affect the performance of Cedana.
