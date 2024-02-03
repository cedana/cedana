sudo rm -rf /tmp/cedana*
sudo rm -rf /var/log/cedana*

sudo pkill gpu-controller 
sudo pkill cdp 
sudo systemctl stop cedana.service
