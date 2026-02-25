set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$SCRIPT_DIR/utils.sh" ]; then
    source "$SCRIPT_DIR/utils.sh"
fi

if ! command -v systemctl &>/dev/null || ! systemctl is-system-running &>/dev/null; then
    pkill -f "$APP_PATH daemon start" || true
    echo "No systemd. Killed any running cedana daemon processes, but no service to remove."
    exit 0
fi

if [ -f "$SERVICE_FILE" ]; then
    echo "Stopping $APP_NAME service..."
    $SUDO_USE systemctl stop "$APP_NAME".service

    # truncate the logs
    echo -n > /var/log/cedana-daemon.log

    $SUDO_USE rm -f "$SERVICE_FILE"
else
    pkill -f "$APP_PATH daemon start" || true
    echo "No systemd service found, but killed any running cedana daemon processes just in case."
    exit 0
fi
