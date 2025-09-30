# Checkpoint/restore streamer

The Cedana daemon supports checkpoint/restore via low-overhead streaming. It's powered by the [streamer plugin](https://github.com/cedana/cedana-image-streamer), which is a fork of CRIU's [image streamer](https://github.com/checkpoint-restore/criu-image-streamer).&#x20;

Real benefit of streaming is realized when checkpointing and restoring to/from a remote location. See [#remoting](cr.md#remoting "mention").

## Prerequisites

1. Create an account with Cedana, to get access to the streamer plugin. See [authentication](../../get-started/authentication.md).
2. Set the Cedana URL & authentication token in the [configuration](../../get-started/configuration.md).
3. Install the streamer plugin with `sudo cedana plugin install streamer`.
4. Ensure the daemon is running, see [installation](../../get-started/installation.md).
5. Do a health check to ensure the plugin is ready, see [health checks](../../get-started/health.md).

## Checkpoint

The `cedana dump` subcommand supports a `--stream <n>` flag, where `n` is the number of parallel streams to use. For example:

```sh
cedana dump process <pid> --stream 4
```

This will directly stream the checkpoint to a directory, using 4 parallel streams. You will notice that the checkpoint directory contains 4 separate image files:

```
-rw-r--r-- 1 root root 145K Feb 19 15:13 img-0
-rw-r--r-- 1 root root  17K Feb 19 15:13 img-1
-rw-r--r-- 1 root root 209K Feb 19 15:13 img-2
-rw-r--r-- 1 root root 188K Feb 19 15:13 img-3
```

## Restore

Similarly, the `cedana restore` subcommand supports a `--stream <n>` flag, where `n` is the number of parallel streams to use. For example:

```sh
cedana restore process --stream 4 --path <path-to-dump>
```

Note that, here you _must_ pass in 4 as the number of parallel streams, as the checkpoint directory contains 4 separate image files, since the checkpoint was taken with 4 parallel streams.

## Compression

All compression algorithms supported for basic checkpoint/restore are supported. See [compression](../cr.md#compression) for more information.

## Remote storage

Cedana supports streaming to/from remote storage, through storage plugins. Check out the following guides for specific remote storage:

- [Amazon S3](storage/s3.md)
- [Google Cloud Storage](storage/gcs.md)
- [Cedana Storage](storage/cedana.md)

{% hint style="info" %}
The daemon simply reads/writes from the filesystem. This is also the case for streaming, with the additional requirement that the underlying filesystem must be [POSIX-compliant](https://grimoire.carcano.ch/blog/posix-compliant-filesystems/). To checkpoint/restore to/from a remote directory, you can also use a FUSE-based filesystem mount backed by your network storage. For Amazon's S3, check out [s3fs-fuse](https://github.com/s3fs-fuse/s3fs-fuse).
{% endhint %}

## Enable by default

To enable streaming by default, set the `Checkpoint.Stream` field in the [configuration](../../get-started/configuration.md) to the desired number of parallel streams. Zero means no streaming.

For all available CLI options, see [CLI reference](../../references/cli/cedana.md). Directly interacting with daemon is also possible through gRPC, see [API reference](../../references/api.md).
