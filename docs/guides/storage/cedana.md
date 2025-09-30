# Cedana Storage

Cedana Storage is a global storage for checkpoints that is backed by multiple cloud providers, providing low latency and high availability. This is the fastest way to get started with remote checkpoint/restore, as you only need to be [authenticated](../../get-started/authentication.md) with Cedana.

{% hint style="warning" %}
If you're using Cedana on an Amazon EKS cluster, you'll likely get higher performance using [Amazon S3](storage/s3.md). Similarly, if you're using Cedana on a GKE cluster, you'll likely get higher performance using [Google Cloud Storage](storage/gcs.md).
{% endhint %}

## Prerequisites

1. Create an account with Cedana, to get access to the GPU plugin. See [authentication](../../get-started/authentication.md).
2. Set the Cedana URL & authentication token in the [configuration](../../get-started/configuration.md).
3. Install the **storage/cedana** plugin with `sudo cedana plugin install storage/cedana`.
4. Ensure the daemon is running, see [installation](../../get-started/installation.md).
5. Do a health check to ensure the plugin is ready, see [health checks](../../get-started/health.md).

## Checkpoint

To checkpoint to Cedana Storage, simply set the `--dir` to a path that starts with `cedana://<path>`, for example:

```sh
cedana dump ... --dir cedana://path/to/dir
```

For example, as explained in [managed checkpoint/restore](../cr.md#managed-checkpoint-restore), to checkpoint a job to Cedana Storage:

```sh
cedana dump job my-job-1 --dir cedana://my-checkpoints
```

If you do `cedana job list`, you will see the latest checkpoint:

```
ID            TIME                 SIZE     PATH
my-job-1      2025-02-19 12:30:36  -        cedana://my-checkpoints/dump-job.tar
```

## Restore

Similarly, to restore from Cedana Storage, simply set the `--path` to your checkpoint path in Cedana Storage, for example:

```sh
cedana restore ... --path cedana://path/to/dump.tar
```

For example, as explained in [managed checkpoint/restore](../cr.md#managed-checkpoint-restore), to restore a job from Cedana Storage:

```sh
cedana restore job --attach my-job-1
```

This will automatically restore from the latest checkpoint for `my-job-1`, which is stored in Cedana Storage.

## Streaming

High-performance low-overhead streaming of checkpoints is also supported by the `storage/cedana` plugin. Follow instructions on [checkpoint/restore streamer](../cr.md#checkpoint-restore-streamer) to use streaming with this plugin.

## See also

- [Amazon S3](storage/s3.md)
- [Google Cloud Storage](storage/gcs.md)
