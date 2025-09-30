# Checkpoint/restore basics

The Cedana daemon is designed to checkpoint/restore processes as well as containers.

## Checkpoint

To checkpoint:

```sh
cedana dump <type> ...
```

Where `<type>` can be `process`, `containerd`, `runc`, `job`, etc. See [feature matrix](../get-started/features.md) for all plugins that support checkpointing.

For example, to checkpoint a process:

```sh
cedana dump process <PID> --dir /tmp
```

A `--dir` flag can be used to specify the _parent_ directory where the checkpoint will be stored. If not provided, the checkpoint will be stored in the default checkpoint directory as specified in the [configuration](../get-started/configuration.md), or in `/tmp` if not set. You may also specify a `--name` flag to give a custom name to the checkpoint file.

See [CLI reference](../references/cli/cedana_dump_process.md) for all available options for process checkpoint.

## Restore

### Using daemon

```sh
cedana restore <type> ...
```

Where `<type>` can be `process`, `containerd`, `runc`, `job`, etc. See [feature matrix](../get-started/features.md) for all plugins that support restoring.

For example, to restore a process:

```sh
cedana restore process --path <path-to-dump>
```

Notice that for restore the flag is called `--path` instead of `--dir` (as in dump), this is because it can be a path to a compressed file, or to a directory if not compressed.

### Without daemon

{% hint style="warning" %}
Not all plugins support restoring without the daemon. Please check the [feature matrix](../get-started/features.md) for details.
{% endhint %}

It's also possible to restore directly as a child of the current shell command without the daemon:

```sh
cedana restore process --path <path-to-dump> --no-server
```

This is useful for scenarios where you want to restore a process as a child of the current shell, for example, to restore a shell process and interact with it directly.

See [CLI reference](../references/cli/cedana_restore_process.md) for all available options for process restore.

## Managed checkpoint/restore

As explained in [managed process/container](managed.md), a job can be of any type, and thus can be checkpointed and restored using the `cedana dump job` and `cedana restore job` subcommands.

The `cedana dump/restore job` subcommands have the same options as their non-managed counterparts, but with pretty good defaults. For e.g., the `--path` flag is not required for `cedana restore job`, as the checkpoint path is stored in the job metadata.

If you do `cedana job list` after checkpointing a job, you will see the latest checkpoint time and size:

```
JOB             TYPE       PID  STATUS  GPU  CHECKPOINT     SIZE     LOG
famous_hopper7  process  32675  halted  no   3 seconds ago  610 KiB
```

To view all checkpoints for a job, use `cedana job checkpoints <job_id>`:

```
ID                                    TIME                 SIZE     PATH
141d52b4-0d1f-4911-a0da-abfab3358d16  2025-02-19 12:32:01  586 KiB  /tmp/dump-process-famous_hopper7-1739986321.tar
386dcce4-a29d-4acb-ab03-12d41b7c42ce  2025-02-19 12:30:36  610 KiB  /tmp/dump-process-famous_hopper7-1739986236.tar
```

## Compression

The `cedana dump` subcommand supports a `--compression` flag to specify the compression algorithm to use. For example:

```sh
cedana dump process <PID> --dir /tmp --name xyz --compression gzip
```

This will create a compressed checkpoint file with the path `/tmp/xyz.tar.gz`. The `--name` flag is optional, and if not provided, the daemon will choose a unique name based on some metadata.

When restoring, the daemon will automatically detect the compression algorithm used and decompress the file. Simply provide the path to the compressed file:

```sh
cedana restore process --path /tmp/xyz.tar.gz
```

Supported values for `--compression` are `none`, `tar`, `gzip`, `lz4`, `zlib`.

You may also specify the default compression algorithm in the [configuration](../get-started/configuration.md).

## Remote storage

Cedana supports checkpointing/restoring to/from remote storage, through storage plugins. Check out the following guides for specific remote storage:

- [Amazon S3](storage/s3.md)
- [Google Cloud Storage](storage/gcs.md)
- [Cedana Storage](storage/cedana.md)

## Advanced

- [Checkpoint/restore with GPUs](gpu/cr.md)
- [Checkpoint/restore runc](runc/cr.md)
- [Checkpoint/restore containerd](runc/cr.md)
- [Checkpoint/restore kata](kata/kata.md)
- [Checkpoint/restore streamer](streamer/cr.md)
- [Checkpoint/restore kubernetes](k8s/cr.md)
