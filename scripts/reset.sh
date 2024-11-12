#!/bin/bash

set -e

# get the directory of the script
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE"  ]; do
    DIR="$( cd -P "$( dirname "$SOURCE"  )" >/dev/null 2>&1 && pwd  )"
    SOURCE="$(readlink "$SOURCE")"
    [[ $SOURCE != /*  ]] && SOURCE="$DIR/$SOURCE"
done
DIR="$( cd -P "$( dirname "$SOURCE"  )" >/dev/null 2>&1 && pwd  )"

source $DIR/utils.sh

USE_SYSTEMCTL=0
for arg in "$@"; do
    if [ "$arg" == "--systemd" ]; then
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
