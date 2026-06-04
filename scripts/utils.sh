# script utils

export APP_NAME="cedana"
if [[ $EUID -ne 0 ]]; then
    export APP_PATH="$HOME/.local/bin/$APP_NAME"
    export CEDANA_PLUGINS_LIB_DIR=${CEDANA_PLUGINS_LIB_DIR:-"$HOME/.local/lib/"}
    export CEDANA_PLUGINS_BIN_DIR=${CEDANA_PLUGINS_BIN_DIR:-"$HOME/.local/bin/"}
else
    export APP_PATH="/usr/local/bin/$APP_NAME"
    export CEDANA_PLUGINS_LIB_DIR=${CEDANA_PLUGINS_LIB_DIR:-"/usr/local/lib/"}
    export CEDANA_PLUGINS_BIN_DIR=${CEDANA_PLUGINS_BIN_DIR:-"/usr/local/bin/"}
fi
export LOG_PATH="/var/log/$APP_NAME-daemon.log"
export SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
export DISABLE_IO_URING=${DISABLE_IO_URING:-true}
export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION="python"

USER=$(whoami)
export USER

ENV=${ENV:-"production"}
if [ -f /.dockerenv ]; then
    ENV="docker"
fi
export ENV

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1" >&2; }
