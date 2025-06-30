# K3s E2E Test on Bare Metal

This guide explains how to run the Cedana K3s checkpoint/restore E2E test on bare metal machines.

## Prerequisites

Before running the test, ensure you have:

- **curl** - for downloading k3s and helm
- **systemctl** - for managing k3s service (should be available on systemd-based systems)
- **sudo access** - required for k3s installation and management
- **bats** - for running the test framework

### Installing BATS (if not already installed)

```bash
# On Ubuntu/Debian
sudo apt-get update && sudo apt-get install -y bats

# On RHEL/CentOS/Fedora
sudo yum install -y bats
# or
sudo dnf install -y bats

# On macOS
brew install bats-core
```

## Running the Test

### Quick Start

Run with default Cedana CI credentials:

```bash
./test/run-k3s-e2e-baremetal.sh
```

### Custom Configuration

Run with custom Cedana credentials:

```bash
./test/run-k3s-e2e-baremetal.sh \
    --token "your-cedana-auth-token" \
    --url "https://your-cedana-instance.com/v1"
```

### Debug Mode

Run with verbose output and preserve k3s for debugging:

```bash
./test/run-k3s-e2e-baremetal.sh --debug --no-cleanup
```

## What the Test Does

1. **Setup Phase:**
   - Installs k3s on bare metal using systemd
   - Installs Helm
   - Deploys Cedana via OCI Helm chart with specified configuration
   - Waits for all components to be ready

2. **Test Phase:**
   - Deploys a test nginx pod
   - Performs checkpoint via Cedana propagator API
   - Deletes the original pod
   - Restores pod from checkpoint
   - Validates the restored pod is functional

3. **Cleanup Phase:**
   - Removes test resources
   - Uninstalls Cedana
   - Removes k3s completely (unless `--no-cleanup` is specified)

## Expected Output

Successful test run should show:

```
✅ k3s cluster and API connectivity verified
✅ Test pod deployed and running  
✅ Checkpoint initiated with action ID: <action-id>
✅ Checkpoint completed with ID: <checkpoint-id>
✅ Original pod deleted
✅ Restore initiated with action ID: <restore-action-id>
✅ Pod restored successfully and is functional
✅ Complete e2e checkpoint/restore workflow validated successfully
```

## Troubleshooting

### Permission Issues

If you get permission errors, ensure your user has sudo access:

```bash
sudo -v  # Test sudo access
```

### K3s Installation Issues

If k3s fails to install, check system requirements:
- Linux kernel 3.10+
- systemd (for service management)
- iptables
- Container runtime requirements

### Cleanup Issues

If the automatic cleanup fails, manually remove k3s:

```bash
sudo /usr/local/bin/k3s-uninstall.sh
```

### Debug Information

For detailed debugging, use the debug flag and check logs:

```bash
./test/run-k3s-e2e-baremetal.sh --debug --no-cleanup

# After test completion, check k3s logs
sudo journalctl -u k3s -f

# Check Cedana pod logs  
kubectl logs -n cedana-systems -l app.kubernetes.io/instance=cedana
```

## Environment Variables

The test uses these environment variables:

- `CEDANA_AUTH_TOKEN` - Authentication token for Cedana API
- `CEDANA_URL` - Base URL for Cedana API (default: https://ci.cedana.ai/v1)

## Important Notes

- This test requires **bare metal execution** - it will not work in Docker containers
- The test requires **root privileges** via sudo for k3s installation
- Running multiple tests simultaneously may cause conflicts
- Always ensure previous k3s installations are cleaned up before running
- The test installs and removes k3s completely - do not run on systems with existing k3s clusters you want to preserve 