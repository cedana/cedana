SUDO_USE=sudo
if ! which sudo &>/dev/null; then
    SUDO_USE=""
fi

USE_SYSTEMCTL=0

for arg in "$@"; do
    if [ "$arg" == "--systemctl" ]; then
        echo "Using systemctl"
        USE_SYSTEMCTL=1
    fi
done

$SUDO_USE rm -rf /tmp/cedana*
$SUDO_USE rm -rf /tmp/sqlite_cedana*
$SUDO_USE rm -rf /var/log/cedana*
$SUDO_USE rm -rf /dev/shm/cedana*

$SUDO_USE pkill cdp
$SUDO_USE pkill otelcol-contrib

if [ $USE_SYSTEMCTL -eq 1 ]; then
    $SUDO_USE systemctl stop cedana.service
else
    $SUDO_USE pkill -2 cedana
fi
