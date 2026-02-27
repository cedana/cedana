# script utils

export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION="python"

export APP_NAME="cedana"
export APP_PATH="/usr/local/bin/$APP_NAME"
export LOG_PATH="/var/log/$APP_NAME-daemon.log"
export SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
export DISABLE_IO_URING=${DISABLE_IO_URING:-true}

USER=$(whoami)
export USER

ENV=${ENV:-production}
if pgrep -f "k3s server" >/dev/null 2>&1; then
    ENV="k3s"
elif [ -f /.dockerenv ]; then
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

check_root() {
    if [[ "$EUID" -ne 0 ]]; then
        echo "This script must be run as root" >&2
        exit 1
    fi
}
