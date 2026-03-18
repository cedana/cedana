# Dynamo Integration via File Watching

This document describes Cedana's file-based checkpoint trigger system for integrating with Dynamo (vLLM, SGLang) workloads.

## Overview

Instead of creating a separate Dynamo plugin, Cedana uses a **config-driven file watcher** that polls container filesystems for trigger files. This provides seamless integration with Dynamo workers without requiring any application code changes.

## Architecture

```
Dynamo Worker                    Cedana File Watcher           Cedana Daemon
    │                                    │                            │
    │ 1. sleep() GPU state              │                            │
    │ 2. write /tmp/ready-for-checkpoint│                            │
    ├────────────────────────────────────>                            │
    │                                    │ 3. detect file (1s poll) │
    │                                    │ 4. trigger checkpoint     │
    │                                    ├───────────────────────────>│
    │                                    │                     5. Freeze
    │                                    │                     6. Dump
    │                                    │                     7. Unfreeze
    │                                    │<───────────────────────────┤
    │ 8. SIGUSR1 sent                   │                            │
    │<────────────────────────────────────                            │
    │ 9. exit                            │                            │
```

## Configuration

File watching is configured in `~/.cedana/config.json` or via environment variables:

### Config File

```json
{
  "file_watching": {
    "enabled": true,
    "poll_interval": "1s",
    "triggers": [
      {
        "path": "/tmp/ready-for-checkpoint",
        "action": "checkpoint",
        "on_success": "SIGUSR1",
        "on_restore": "SIGCONT",
        "on_failure": "SIGKILL"
      }
    ]
  }
}
```

### Environment Variables

```bash
CEDANA_FILE_WATCHING_ENABLED=true
CEDANA_FILE_WATCHING_POLL_INTERVAL=1s
CEDANA_FILE_WATCHING_TRIGGERS='[{"path":"/tmp/ready-for-checkpoint","action":"checkpoint","onSuccess":"SIGUSR1","onRestore":"SIGCONT","onFailure":"SIGKILL"}]'
```

## Kubernetes Deployment

File watching is integrated into the existing `cedana-helm` chart. Simply enable it in your values:

```yaml
# values.yaml
config:
  fileWatching:
    enabled: true
    pollInterval: "1s"
    triggers:
      - path: "/tmp/ready-for-checkpoint"
        action: "checkpoint"
        onSuccess: "SIGUSR1"
        onRestore: "SIGCONT"
        onFailure: "SIGKILL"
```

Or use the provided example:

```bash
helm install cedana cedana-helm/ -f cedana-helm/values-dynamo.yaml \
  --set config.authToken=<token> \
  --set config.clusterId=<cluster>
```

## How It Works

### Checkpoint Flow

1. **Worker signals readiness**: Dynamo worker calls `sleep()` to drain GPU state, then writes `/tmp/ready-for-checkpoint`
2. **File watcher detects**: Cedana polls containers every 1s (configurable) and finds the trigger file
3. **Checkpoint triggered**: Cedana performs Freeze → Dump → Unfreeze via daemon APIs
4. **Signal sent**: On success, SIGUSR1 sent to worker process; file removed
5. **Worker exits**: Worker receives SIGUSR1 and exits gracefully

### Restore Flow

1. **Pod starts**: Dynamo creates restore pod with placeholder container
2. **Restore triggered**: Cedana restores from checkpoint (via RabbitMQ or other trigger)
3. **Signal sent**: On completion, SIGCONT sent to restored process
4. **Worker continues**: Worker calls `wake_up()` and resumes serving

## Signal Protocol

| Signal | When | Purpose |
|--------|------|---------|
| `SIGUSR1` | After successful checkpoint | Tell worker checkpoint is complete, safe to exit |
| `SIGCONT` | After successful restore | Tell worker restore is complete, resume execution |
| `SIGKILL` | After checkpoint failure | Immediately terminate (GPU may be locked) |

## Supported Signals

The file watcher supports standard POSIX signals:
- `SIGUSR1`, `USR1`
- `SIGUSR2`, `USR2`
- `SIGCONT`, `CONT`
- `SIGTERM`, `TERM`
- `SIGKILL`, `KILL`
- `SIGHUP`, `HUP`
- Numeric signals (1-31)

## Implementation Details

### Code Location

- **File watcher**: `plugins/k8s/internal/filewatcher/watcher.go`
- **Config types**: `pkg/config/types.go`
- **K8s integration**: `plugins/k8s/cmd/helper.go` (auto-starts if enabled)

**Note**: The file watcher is in the K8s plugin (not `pkg/`) because it's K8s-specific—it accesses container rootfs via containerd/CRI-O bundle paths.

### Container Filesystem Access

The file watcher accesses container filesystems via the runc bundle path:
```go
rootfs := container.GetRunc().GetBundle() + "/rootfs"
triggerPath := rootfs + trigger.Path
```

For a container with bundle at `/run/containerd/io.containerd.runtime.v2.task/k8s.io/abc123/`, the watcher checks:
```
/run/containerd/.../abc123/rootfs/tmp/ready-for-checkpoint
```

### Polling vs Events

The watcher uses **polling** (not inotify) because:
1. Works across all container runtimes (containerd, CRI-O, etc.)
2. Simpler implementation (no namespace hopping)
3. Scalable (1s poll on 100 containers = minimal overhead)
4. Reliable (no missed events from inotify queue limits)

Default poll interval is 1 second, configurable via `poll_interval`.

## Comparison with Dynamo CRIU

| Feature | Dynamo CRIU DaemonSet | Cedana File Watcher |
|---------|----------------------|---------------------|
| Trigger mechanism | K8s pod readiness probe | File polling |
| Checkpoint tool | CRIU binary | Cedana daemon |
| GPU support | cuda-checkpoint binary | Cedana GPU middleware |
| Signal protocol | ✅ SIGUSR1/SIGCONT | ✅ SIGUSR1/SIGCONT |
| Worker changes | None | None |
| Manifest format | Dynamo manifest.yaml | Cedana checkpoint metadata |
| Multi-container | ✅ | ✅ |

## Performance

**Overhead**:
- **CPU**: Negligible (<0.1% per container)
- **Latency**: 1 second average (poll interval / 2)
- **Memory**: ~1 MB per watcher

**Scaling**:
- 100 containers × 1s poll = 100 stat() calls/sec (trivial)
- Recommend increasing poll interval to 2-5s for clusters with 500+ containers per node

## Troubleshooting

### File not detected

```bash
# Check file exists in container
kubectl exec <pod> -- ls -la /tmp/ready-for-checkpoint

# Check watcher logs
kubectl logs -l app=cedana-helper | grep "trigger file detected"

# Verify rootfs path
kubectl exec <cedana-helper-pod> -- ls /run/containerd/.../rootfs/tmp/
```

### Signal not delivered

```bash
# Check PID resolution
kubectl logs -l app=cedana-helper | grep "sent signal"

# Verify process is running
kubectl exec <pod> -- ps aux | grep python
```

### Checkpoint fails

```bash
# Daemon logs
kubectl exec <cedana-helper-pod> -- journalctl -u cedana -f

# Check storage
kubectl exec <cedana-helper-pod> -- df -h /checkpoints
```

## Examples

### Multiple Triggers

```yaml
fileWatching:
  enabled: true
  pollInterval: "500ms"
  triggers:
    # Checkpoint trigger
    - path: "/tmp/ready-for-checkpoint"
      action: "checkpoint"
      onSuccess: "SIGUSR1"
      onFailure: "SIGKILL"
    # Restore trigger (optional - usually RabbitMQ handles this)
    - path: "/tmp/restore-from-checkpoint"
      action: "restore"
      onRestore: "SIGCONT"
      onFailure: "SIGTERM"
```

### Custom Signals

```yaml
triggers:
  - path: "/app/checkpoint-ready"
    action: "checkpoint"
    onSuccess: "SIGUSR2"  # Custom signal for your app
    onRestore: "SIGHUP"
    onFailure: "SIGTERM"  # Graceful termination instead of SIGKILL
```

## Migration from Dynamo CRIU

1. **Deploy Cedana** with file watching enabled:
   ```bash
   helm install cedana cedana-helm/ -f cedana-helm/values-dynamo.yaml
   ```

2. **Verify** checkpoints work:
   ```bash
   # Create test checkpoint
   kubectl apply -f dynamo-checkpoint.yaml
   kubectl logs <checkpoint-pod> -f
   ```

3. **Remove CRIU DaemonSet**:
   ```bash
   kubectl delete daemonset dynamo-snapshot-agent
   ```

No changes needed to:
- ✅ Dynamo operator
- ✅ DynamoCheckpoint CRDs
- ✅ Worker code
- ✅ Checkpoint manifests

## Future Enhancements

Potential improvements (not currently implemented):
- inotify support for sub-second latency
- Batched checkpoint operations
- Checkpoint scheduling (time-based, resource-based)
- Automatic trigger file cleanup on pod deletion

## References

- [File Watcher README](../plugins/k8s/internal/filewatcher/README.md)
- [Cedana Helm Chart](https://github.com/cedana/cedana-helm-charts/tree/main/cedana-helm)
- [Dynamo Snapshot Python Code](https://github.com/ai-dynamo/dynamo/tree/main/components/src/dynamo)
