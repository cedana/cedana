# Profiling

The profiling system tries to be contextual and _invisible_. Profiling data is sent as [gRPC Metadata](https://grpc.io/docs/guides/metadata/). To enable, set `Profiling.Enabled=true` in [configuration](../get-started/configuration.md).

Since each adapter to a request (see [architecture](architecture.md)) is a well-defined single-responsibility function, it makes sense to profile each of them. When enabled, the daemon will profile each adapter, and send back flattened data as a gRPC trailer. For readability's sake, this profiling is completely invisible and handled by the adapter logic in `pkg/types/adapter.go` (also see `pkg/types/timer.go`). This flattened data is parsed by the `cmd` package and displayed as shown below. An example out of `cedana dump containerd ...` when profiling is enabled:

![Output from a cedana dump containerd](https://github.com/user-attachments/assets/977a5423-e4d3-423e-89af-653c72bfce03)

Above, you can see complete flow of the request before it reaches CRIU's dump function, including _which_ plugin (2nd column) the adapter belongs to. For `containerd`, you can see that the request is largely handled by the low-level runtime `runc`'s plugin. The second table above shows a compressed view of the same data, with only the total time spent in each plugin/category. Note that, above there are some components that are executed concurrently, e.g. `rootfs`, so the time you see in the (flattened) data above is only the time spent _waiting_ for `rootfs` to finish.

In many cases, we may need to add more context to this data, or add more components to it. Helpers defined in `pkg/profiling/timing.go` can be used. A good example is adding a new component to the `compression` category in `internal/server/filesystem/dump_adapters.go` as seen above:

{% @github-files/github-code-block url="https://github.com/cedana/cedana/blob/9fa628d372cd71ba3bfea3437c9e3e4dc0a0bbe5/internal/cedana/filesystem/dump_adapters.go#L113-L115" %}

These helpers use the passed `context` to store profiling data. If the `context` already has parent profiling data, the data is added as a component to the parent.

{% hint style="info" %}
Behind the scenes, if metrics is enabled ([configuration](../get-started/configuration.md) `Metrics=true`), this data is also captured as OTel spans.
{% endhint %}

## Restore profiling and metadata

Restore uses the same profiling plumbing as dump, but adds two restore-specific outputs:

- `--profiling-path` writes the flattened profiling JSON to a local file.
- `--profiling-upload-path` stores the profiling JSON at an explicit storage path instead of deriving one from the restore source path.
- `--upload-profiling` uploads the profiling JSON using the selected storage backend.
- If `--profiling-upload-path` is omitted, cedana derives `restore-<uuid>.json` from `--restore-path` and writes there.

The uploaded object name is derived from the restore UUID and is written as `restore-<uuid>.json`. For compressed archives, the file is placed next to the archive rather than inside it.

Restore notifications can also carry request and runtime metadata when `--notify` is enabled. The flags `--metadata`, `--request-metadata`, `--runtime-metadata`, and `--notification-name` are copied into the restore event payload. If profiling upload fails, the payload also carries the upload error so downstream consumers can distinguish it from the restore error itself.
