USE_SYSTEMCTL=0

for arg in "$@"; do
    if [ "$arg" == "--systemctl" ]; then
        echo "Using systemctl"
        USE_SYSTEMCTL=1
    fi
done

sudo rm -rf /tmp/cedana*
sudo rm -rf /tmp/sqlite_cedana*
sudo rm -rf /var/log/cedana*
sudo rm -rf /dev/shm/cedana*

sudo pkill cdp
sudo pkill otelcol-contrib

if [ $USE_SYSTEMCTL -eq 1 ]; then
    sudo systemctl stop cedana.service
else
    sudo pkill -2 cedana
fi
