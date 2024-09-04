#!/bin/bash
# shellcheck disable=SC2181
#
set -e

SUDO_USE=sudo
if ! which sudo &>/dev/null; then
    SUDO_USE=""
fi

# Define variables
APP_NAME="cedana"
APP_PATH="/usr/local/bin/$APP_NAME"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
USER=$(whoami)
CEDANA_OTEL_ENABLED=${CEDANA_OTEL_ENABLED:-false}
CEDANA_GPU_CONTROLLER_PATH="/usr/local/bin/cedana-gpu-controller"
CEDANA_PROFILING_ENABLED=${CEDANA_PROFILING_ENABLED:-0}
CEDANA_IS_K8S=${CEDANA_IS_K8S:-0}
CEDANA_GPU_ENABLED=false
CEDANA_GPU_DEBUGGING_ENABLED=${CEDANA_GPU_DEBUGGING_ENABLED:-0}
USE_SYSTEMCTL=0
NO_BUILD=0
DAEMON_ARGS=""

# Check for --systemctl flag
for arg in "$@"; do
    if [ "$arg" == "--systemctl" ]; then
        echo "Using systemctl"
        USE_SYSTEMCTL=1
    fi
    if [ "$arg" == "--no-build" ]; then
        echo "Skipping build"
        NO_BUILD=1
    fi
    if [ "$arg" == "--gpu" ]; then
        echo "GPU support enabled"
        CEDANA_GPU_ENABLED=true
    fi
    if [[ $arg == --args=* ]]; then
        value="${arg#*=}"
        echo "Daemon args: $value"
        DAEMON_ARGS="$value"
    fi

    if [ "$arg" == "--otel" ]; then
        echo "otel enabled, starting otelcol.."
        CEDANA_OTEL_ENABLED=true
    fi
done

export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION="python"

if [ $NO_BUILD -ne 1 ]; then
    echo "Building $APP_NAME..."
    VERSION=$(git describe --tags --always)
    LDFLAGS="-X main.Version=$VERSION"

    CGO_ENABLED=1 go build -ldflags "$LDFLAGS"

    if [ $? -ne 0 ]; then
        echo "Build failed. Exiting."
        exit 1
    else
        echo "Build successful. Copying the cedana binary"
        $SUDO_USE cp $APP_NAME $APP_PATH
    fi
else
    echo "Skipping build..."
    if test -f $APP_NAME; then
        echo "Found binary to copy."
        $SUDO_USE cp $APP_NAME $APP_PATH
    else
        echo "Moving forward without copy."
    fi
fi

if [ "$CEDANA_GPU_ENABLED" = "true" ]; then
    echo "Starting daemon with GPU support..."
fi

if [ "$CEDANA_GPU_DEBUGGING_ENABLED" = "true" ]; then
    echo "Starting daemon with GPU debugging support..."
fi

if test -f $SERVICE_FILE; then
    echo "Restarting $APP_NAME..."
fi

if [ $USE_SYSTEMCTL -eq 1 ]; then
    # create systemd file
    echo "Creating $SERVICE_FILE..."
    cat <<EOF | $SUDO_USE tee $SERVICE_FILE >/dev/null
[Unit]
Description=Cedana Checkpointing Daemon
[Service]
Environment=USER=$USER
Environment=CEDANA_GPU_CONTROLLER_PATH=$CEDANA_GPU_CONTROLLER_PATH
Environment=CEDANA_PROFILING_ENABLED=$CEDANA_PROFILING_ENABLED
Environment=CEDANA_OTEL_ENABLED=$CEDANA_OTEL_ENABLED
Environment=CEDANA_IS_K8S=$CEDANA_IS_K8S
Environment=CEDANA_GPU_DEBUGGING_ENABLED=$CEDANA_GPU_DEBUGGING_ENABLED
ExecStart=$APP_PATH daemon start --gpu-enabled=$CEDANA_GPU_ENABLED
User=root
Group=root
Restart=no

[Install]
WantedBy=multi-user.target

[Service]
StandardError=append:/var/log/cedana-daemon.log
StandardOutput=append:/var/log/cedana-daemon.log
EOF

    echo "Reloading systemd..."
    $SUDO_USE systemctl daemon-reload

    echo "Enabling and starting $APP_NAME service..."
    $SUDO_USE systemctl enable $APP_NAME.service
    $SUDO_USE systemctl start $APP_NAME.service
    echo "$APP_NAME service setup complete."
else
    echo "Starting daemon as a background process..."
    if [[ -z "${SUDO_USE}" ]]; then
        # only systemctl writes to /var/log/cedana-daemon.log, if starting w/out systemctl
        # still want to write logs to a file
        $APP_PATH daemon start --gpu-enabled="$CEDANA_GPU_ENABLED" "$DAEMON_ARGS" 2>&1 | tee -a /var/log/cedana-daemon.log &
    else
        $SUDO_USE -E $APP_PATH daemon start --gpu-enabled="$CEDANA_GPU_ENABLED" "$DAEMON_ARGS" 2>&1 | tee -a /var/log/cedana-daemon.log &
    fi
    echo "$APP_NAME daemon started as a background process."
fi
