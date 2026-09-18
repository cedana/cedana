# Cedana Storage Express (CSX)

Cedana Storage Express (CSX) is Cedana's NFS-backed storage solution. It uses tmpfs, local disk and NFS to efficiently do checkpoint/restore.
The **storage/csx** plugin lets the Cedana daemon checkpoint and restore through a local CSX daemon.

## Prerequisites

1. Authenticate with Cedana to access released plugins. See [authentication](../../get-started/authentication.md).
2. Install the **storage/csx** plugin with `sudo cedana plugin install storage/csx`.
3. Install the **csx** binary with `sudo cedana plugin install csx`.
4. Ensure the Cedana daemon is running. See [installation](../../get-started/installation.md).

## Start the CSX daemon

Once the Cedana daemon is running, start the CSX daemon:

```sh
sudo ./csx
```

The CSX daemon requires root privileges because it creates `tmpfs` mounts. The **storage/csx** plugin connects to its Unix socket at `/run/csx.sock`, so the CSX daemon must remain running while checkpointing or restoring.
Complete documentation for the CSX daemon is work in progress.

## Checkpoint

To checkpoint to CSX, set `--dir` to `csx://`:

```sh
cedana dump ... --dir csx://
```

For example, from a Cedana source checkout, start the included test workload as a managed job:

```sh
cedana exec ./test/workloads/date-loop.sh -j test -a
```

Then checkpoint the job from another terminal:

```sh
cedana dump job test --dir csx://
```

Cedana records the generated checkpoint path in the job metadata with the `csx://` prefix. This prefix ensures that subsequent operations automatically use the **storage/csx** plugin.

## Restore

As explained in [managed checkpoint/restore](../cr.md#managed-checkpoint-restore), a managed job can be restored without specifying its checkpoint path:

```sh
cedana restore job test
```

Cedana reads the latest checkpoint path from the job metadata and automatically restores it through CSX.

To restore an unmanaged checkpoint, pass its recorded CSX path explicitly:

```sh
cedana restore ... --path csx://<dump-path>
```

## Compression

All compression algorithms supported for basic checkpoint/restore are supported. See [compression](../cr.md#compression) for more information.

## Streaming

Checkpoint/restore streaming is not currently supported by the **storage/csx** plugin. Leave `--streams` unset or set it to `0` when using CSX.

## Enable by default

To use CSX without passing `--dir csx://` for every checkpoint, set `CEDANA_CHECKPOINT_DIR=csx://` in the Cedana daemon's environment.

Alternatively, set `Checkpoint.Dir` in the [daemon configuration](../../get-started/configuration.md):

```json
{
  "checkpoint": {
    "dir": "csx://"
  }
}
```

Restart the Cedana daemon after changing its environment or configuration.

## See also

- [Amazon S3](s3.md)
- [Google Cloud Storage](gcs.md)
- [Cedana Storage](cedana.md)
