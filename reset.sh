SUDO_USE=sudo
if ! which sudo &>/dev/null; then
    SUDO_USE=""
fi

if [ -f /etc/systemd/system/cedana.service ]; then
    echo "Stopping Cedana service..."
    $SUDO_USE systemctl stop cedana.service
else
    echo "Cedana service not found. Assuming Cedana daemon is running without systemd."
    $SUDO_USE pkill -2 cedana
fi

if [ -f /var/log/cedana-daemon.log ]; then
    $SUDO_USE pkill cdp
    $SUDO_USE pkill otelcol-contrib

    # run cedana script only exists if cedana is installed through setup-host.sh which is only used
    # for k8s setups
    if [ -f /cedana/scripts/run-cedana.sh ]; then
        # remove Cedana binaries
        $SUDO_USE rm -f /usr/local/bin/cedana /usr/local/bin/cedana-gpu-controller /usr/local/lib/libcedana.so
    fi

    $SUDO_USE rm -rf /cedana
    $SUDO_USE rm -rf /criu
    $SUDO_USE rm -rf /tmp/cedana*
    $SUDO_USE rm -rf /tmp/sqlite_cedana*
    $SUDO_USE rm -rf /var/log/cedana*
    $SUDO_USE rm -rf /dev/shm/cedana*

    # remove Cedana systemd service
    $SUDO_USE rm -f /etc/systemd/system/cedana.service
fi
