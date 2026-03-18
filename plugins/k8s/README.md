## Kubernetes Plugin

This plugin provides Kubernetes integration for Cedana, including DaemonSet helpers, event streaming, and file-based checkpoint triggers.

## Components

### Helper DaemonSet (`cmd/helper.go`)

Helper commands to setup and manage Cedana inside a Kubernetes environment.

**Commands:**
- `cedana k8s setup` - Sets up a node by installing prerequisites, binaries, and starting the daemon
- `cedana k8s destroy` - Cleans up a node after Cedana is stopped

**Features:**
- Installs Cedana daemon on host nodes
- Starts RabbitMQ event consumer for orchestrated checkpoints
- Optionally starts file watcher for Dynamo-style triggers (if `file_watching.enabled: true`)
- Tails daemon logs to stdout

### File Watcher (`internal/filewatcher/`)

Polls container filesystems for trigger files to initiate checkpoints. See [File Watcher README](internal/filewatcher/README.md).

**Use case:** Dynamo (vLLM, SGLang) integration where workers write `/tmp/ready-for-checkpoint` when ready.

### Event Stream (`internal/eventstream/`)

RabbitMQ consumer for processing checkpoint requests from Cedana propagator.

## Deployment

Typically deployed as a DaemonSet via the [cedana-helm chart](https://github.com/cedana/cedana-helm-charts/tree/main/cedana-helm):

```bash
helm install cedana cedana-helm/ \
  --set config.authToken=<token> \
  --set config.clusterId=<cluster>
```

For Dynamo integration, enable file watching:

```bash
helm install cedana cedana-helm/ -f cedana-helm/values-dynamo.yaml
```

## Documentation

- [File Watcher Documentation](internal/filewatcher/README.md)
- [Dynamo Integration Guide](../../docs/dynamo-integration.md)
