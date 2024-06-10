sudo rm -rf /tmp/cedana*
sudo rm -rf /tmp/sqlite_cedana*
sudo rm -rf /var/log/cedana*
sudo rm -rf /dev/shm/cedana*

sudo pkill gpu-controller 
sudo pkill cdp 
sudo pkill otelcol-contrib
sudo systemctl stop cedana.service
