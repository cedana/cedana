#!/bin/bash

##################################################
### Debug Cedana Host Access in K3s Environment ###
##################################################

set -e

echo "=== Debugging Cedana Helper Host Access ==="
echo "Timestamp: $(date)"
echo ""

# Get helper pod name
HELPER_POD=$(kubectl get pods -n cedana-systems -l app.kubernetes.io/component=helper -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$HELPER_POD" ]; then
    echo "❌ No Cedana helper pod found"
    kubectl get pods -n cedana-systems || true
    exit 1
fi

echo "📍 Helper pod: $HELPER_POD"

# Check helper pod status
HELPER_STATUS=$(kubectl get pod "$HELPER_POD" -n cedana-systems -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
RESTART_COUNT=$(kubectl get pod "$HELPER_POD" -n cedana-systems -o jsonpath='{.status.containerStatuses[0].restartCount}' 2>/dev/null || echo "0")

echo "📊 Helper status: $HELPER_STATUS (restarts: $RESTART_COUNT)"
echo ""

if [ "$HELPER_STATUS" != "Running" ]; then
    echo "🚨 Helper pod is not running! Status: $HELPER_STATUS"
    echo ""
    echo "=== Pod Description ==="
    kubectl describe pod "$HELPER_POD" -n cedana-systems | tail -20
    echo ""
    echo "=== Current Pod Logs ==="
    kubectl logs "$HELPER_POD" -n cedana-systems --tail=50 2>/dev/null || echo "❌ Cannot get current logs"
    echo ""
    echo "=== Previous Pod Logs (if available) ==="
    kubectl logs "$HELPER_POD" -n cedana-systems --previous --tail=50 2>/dev/null || echo "❌ No previous logs available"
    echo ""
    echo "=== Recent Events ==="
    kubectl get events -n cedana-systems --field-selector involvedObject.name="$HELPER_POD" --sort-by='.lastTimestamp' 2>/dev/null || echo "❌ Cannot get events"
    echo ""
    echo "❌ Cannot perform host access checks - helper pod is not running"
    exit 1
fi

echo "=== 1. Host Filesystem Access ==="
echo "Checking what the helper can see of the host filesystem..."

# Check if /host is mounted and accessible
kubectl exec $HELPER_POD -n cedana-systems -- ls -la /host/ | head -10

echo ""
echo "=== 2. Distribution Detection Files ==="
echo "Checking common distribution detection files..."

# Check /etc/os-release (most common)
echo "--- /host/etc/os-release ---"
kubectl exec $HELPER_POD -n cedana-systems -- cat /host/etc/os-release 2>/dev/null || echo "❌ Not accessible"

echo ""
echo "--- /host/etc/lsb-release ---"
kubectl exec $HELPER_POD -n cedana-systems -- cat /host/etc/lsb-release 2>/dev/null || echo "❌ Not accessible"

echo ""
echo "--- /host/etc/debian_version ---"
kubectl exec $HELPER_POD -n cedana-systems -- cat /host/etc/debian_version 2>/dev/null || echo "❌ Not accessible"

echo ""
echo "--- /host/etc/redhat-release ---"
kubectl exec $HELPER_POD -n cedana-systems -- cat /host/etc/redhat-release 2>/dev/null || echo "❌ Not accessible"

echo ""
echo "=== 3. Systemd Access ==="
echo "Checking systemd accessibility..."

# Check if systemd directory exists
echo "--- /host/etc/systemd ---"
kubectl exec $HELPER_POD -n cedana-systems -- ls -la /host/etc/systemd/ 2>/dev/null || echo "❌ Not accessible"

echo ""
echo "--- /host/run/systemd ---"
kubectl exec $HELPER_POD -n cedana-systems -- ls -la /host/run/systemd/ 2>/dev/null || echo "❌ Not accessible"

echo ""
echo "--- /host/lib/systemd ---"
kubectl exec $HELPER_POD -n cedana-systems -- ls -la /host/lib/systemd/ 2>/dev/null || echo "❌ Not accessible"

echo ""
echo "=== 4. Process Namespace Check ==="
echo "Checking if helper can see host processes..."

# Check PID 1 (should be systemd on host)
echo "--- PID 1 (should be systemd) ---"
kubectl exec $HELPER_POD -n cedana-systems -- ps -p 1 -o pid,cmd 2>/dev/null || echo "❌ Cannot see PID 1"

echo ""
echo "--- All processes (first 10) ---"
kubectl exec $HELPER_POD -n cedana-systems -- ps aux | head -10 2>/dev/null || echo "❌ Cannot see processes"

echo ""
echo "=== 5. Network Namespace Check ==="
echo "Checking network access..."

# Check hostname
echo "--- Hostname ---"
kubectl exec $HELPER_POD -n cedana-systems -- hostname 2>/dev/null || echo "❌ Cannot get hostname"

echo ""
echo "--- Host's hostname (from /host/etc/hostname) ---"
kubectl exec $HELPER_POD -n cedana-systems -- cat /host/etc/hostname 2>/dev/null || echo "❌ Not accessible"

echo ""
echo "=== 6. Capabilities and Security Context ==="
echo "Checking container capabilities..."

# Check effective capabilities
echo "--- Effective capabilities ---"
kubectl exec $HELPER_POD -n cedana-systems -- cat /proc/self/status | grep Cap 2>/dev/null || echo "❌ Cannot read capabilities"

echo ""
echo "--- User ID ---"
kubectl exec $HELPER_POD -n cedana-systems -- id 2>/dev/null || echo "❌ Cannot get user info"

echo ""
echo "=== 7. Mount Information ==="
echo "Checking mounts..."

echo "--- Container mounts ---"
kubectl exec $HELPER_POD -n cedana-systems -- mount | grep -E "(/host|proc|sys)" | head -5 2>/dev/null || echo "❌ Cannot see mounts"

echo ""
echo "=== 8. Host System Commands ==="
echo "Testing if common system commands work..."

# Try to run systemctl from host
echo "--- systemctl status (chrooted) ---"
kubectl exec $HELPER_POD -n cedana-systems -- chroot /host systemctl --version 2>/dev/null || echo "❌ Cannot run systemctl in chroot"

echo ""
echo "--- which systemctl (on host) ---"
kubectl exec $HELPER_POD -n cedana-systems -- chroot /host which systemctl 2>/dev/null || echo "❌ systemctl not found in chroot"

echo ""
echo "=== 9. Container vs Host Environment ==="
echo "Comparing container and host environments..."

echo "--- Container /etc/os-release ---"
kubectl exec $HELPER_POD -n cedana-systems -- cat /etc/os-release 2>/dev/null || echo "❌ Not accessible"

echo ""
echo "--- Container vs Host filesystem root ---"
echo "Container root:"
kubectl exec $HELPER_POD -n cedana-systems -- ls -la / | head -5 2>/dev/null || echo "❌ Cannot list container root"

echo ""
echo "Host root:"
kubectl exec $HELPER_POD -n cedana-systems -- ls -la /host/ | head -5 2>/dev/null || echo "❌ Cannot list host root"

echo ""
echo "=== 10. Cedana Helper Logs ==="
echo "Recent helper logs..."
kubectl logs $HELPER_POD -n cedana-systems --tail=20

echo ""
echo "=== Debug Complete ==="
echo "This debug information shows what the Cedana helper can access in the k3s environment" 