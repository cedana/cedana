# script utils

export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION="python"

SUDO_USE="sudo -E"
if ! which sudo &>/dev/null; then
    export SUDO_USE=""
fi

export APP_NAME="cedana"
export APP_PATH="/usr/local/bin/$APP_NAME"
export SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
USER=$(whoami)
export USER
