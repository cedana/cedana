# File Watcher (K8s Plugin)

The file watcher provides file-based checkpoint triggers for Kubernetes workloads. This enables integration with frameworks like Dynamo that signal checkpoint readiness by writing files to the container filesystem.

**Note**: This is part of the `k8s` plugin because it accesses container rootfs paths via containerd/CRI-O bundle directories. It's not a generic file watcher.

## Overview

Instead of using K8s labels/annotations or RabbitMQ events, the file watcher polls container filesystems (via runc bundle rootfs) for trigger files. When a file appears, it automatically triggers checkpoint/restore operations and sends completion signals.

## Configuration

Add to your `~/.cedana/config.json`:

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

Or via environment variables:

```bash
CEDANA_FILE_WATCHING_ENABLED=true
CEDANA_FILE_WATCHING_POLL_INTERVAL=1s
```

## Configuration Fields

- **`enabled`**: Enable/disable file watching (default: `false`)
- **`poll_interval`**: How often to check for trigger files (e.g., `"1s"`, `"500ms"`)
- **`triggers`**: Array of trigger configurations

### Trigger Configuration

- **`path`**: File path to watch (relative to container filesystem, e.g., `/tmp/ready-for-checkpoint`)
- **`action`**: What to do when file appears (`"checkpoint"` or `"restore"`)
- **`on_success`**: Signal to send on successful checkpoint (e.g., `"SIGUSR1"`)
- **`on_restore`**: Signal to send after restore completes (e.g., `"SIGCONT"`)
- **`on_failure`**: Signal to send on failure (e.g., `"SIGKILL"`, `"SIGTERM"`)

## Supported Signals

- `SIGUSR1`, `USR1`
- `SIGUSR2`, `USR2`
- `SIGCONT`, `CONT`
- `SIGTERM`, `TERM`
- `SIGKILL`, `KILL`
- `SIGHUP`, `HUP`
- Numeric signals (1-31)

## How It Works

### Checkpoint Flow

1. Worker writes readiness file (e.g., `/tmp/ready-for-checkpoint`)
2. File watcher detects file during polling interval
3. Cedana performs checkpoint: Freeze → Dump → Unfreeze
4. On success: removes trigger file and sends `on_success` signal (e.g., `SIGUSR1`)
5. On failure: sends `on_failure` signal (e.g., `SIGKILL`)

### Restore Flow

1. Worker writes restore file containing checkpoint path (e.g., `/tmp/restore-from-checkpoint`)
2. File watcher detects file
3. Cedana restores from checkpoint
4. On success: removes trigger file and sends `on_restore` signal (e.g., `SIGCONT`)
5. On failure: sends `on_failure` signal

## Integration with Dynamo

This provides a drop-in replacement for Dynamo's CRIU-based checkpoint flow:

```yaml
# cedana config.json
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

Dynamo workers (vLLM, SGLang) already write `/tmp/ready-for-checkpoint` and listen for `SIGUSR1`/`SIGCONT` signals, so no application changes are needed.

## Usage with K8s Helper

The file watcher integrates with `cedana k8s setup`:

```bash
# In your DaemonSet or helper pod
cedana k8s setup
```

The helper will:
1. Install Cedana daemon
2. Start RabbitMQ event consumer (if configured)
3. Start file watcher (if `file_watching.enabled: true`)

Both systems can run simultaneously - use RabbitMQ for orchestrated checkpoints and file watching for application-triggered checkpoints.

## Example: Dynamo Worker

```python
# In your vLLM/SGLang worker
import signal
import os

# Signal checkpoint readiness
with open("/tmp/ready-for-checkpoint", "w") as f:
    f.write("ready")

# Wait for SIGUSR1 (checkpoint complete)
signal.pause()
```

The file watcher will:
1. Detect the `/tmp/ready-for-checkpoint` file
2. Trigger checkpoint via Cedana
3. Send `SIGUSR1` to the worker on completion
4. Worker receives signal and exits

## Performance Considerations

- **Poll interval**: Lower = faster response, higher = less CPU overhead. `1s` is recommended.
- **Multiple triggers**: All triggers are checked on each poll. Keep the list small for best performance.
- **Container count**: Polling scales linearly with number of containers. On large clusters (100+ containers per node), consider increasing `poll_interval` to `2s` or `5s`.

## Troubleshooting

**File watcher not starting:**
- Check logs: `kubectl logs -l app=cedana-helper -f`
- Verify config: `cat ~/.cedana/config.json | jq .file_watching`
- Ensure `file_watching.enabled: true`

**Trigger file detected but no checkpoint:**
- Check Cedana daemon logs: `journalctl -u cedana -f`
- Verify container has correct rootfs path
- Check file watcher logs for errors

**Signals not delivered:**
- Verify PID is correct in logs
- Check if process is still running: `ps aux | grep <pid>`
- Ensure signal name is valid (case-sensitive)
