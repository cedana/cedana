#!/bin/bash

set -eo pipefail

# get the directory of the script
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE"  ]; do
    DIR="$( cd -P "$( dirname "$SOURCE"  )" >/dev/null 2>&1 && pwd  )"
    SOURCE="$(readlink "$SOURCE")"
    [[ $SOURCE != /*  ]] && SOURCE="$DIR/$SOURCE"
done
DIR="$( cd -P "$( dirname "$SOURCE"  )" >/dev/null 2>&1 && pwd  )"

source "$DIR"/utils.sh

if ! command -v systemctl &>/dev/null; then
    echo "No systemctl on this system."
    exit
fi

if [ -f "$SERVICE_FILE" ]; then
    echo "Stopping $APP_NAME service..."
    $SUDO_USE systemctl stop "$APP_NAME".service

    # truncate the logs
    echo -n > /var/log/cedana-daemon.log

    $SUDO_USE rm -f "$SERVICE_FILE"
else
    echo "No systemd service found"
fi
