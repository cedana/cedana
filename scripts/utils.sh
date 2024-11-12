# script utils

export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION="python"

SUDO_USE="sudo -E"
if ! which sudo &>/dev/null; then
    SUDO_USE=""
fi

APP_NAME="cedana"
APP_PATH="/usr/local/bin/$APP_NAME"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
USER=$(whoami)
