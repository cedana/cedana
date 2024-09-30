echo "\033[1msudo rm -rf /test\033[0m"
sudo rm -rf /test
echo "\033[1msudo rm /var/log/cedana*\033[0m"
sudo rm /var/log/cedana*
echo "\033[1msudo ./reset.sh --systemctl\033[0m"
sudo ./reset.sh --systemctl
echo "\033[1m./build-start-daemon.sh --systemctl --gpu-enabled\033[0m"
./build-start-daemon.sh --systemctl --gpu-enabled
echo "\033[1msleep 1\033[0m"
sleep 1
echo "\033[1mcedana exec -i ced -w /home/ubuntu/nanoGPT "python3 sample.py --init_from=gpt2 --start='tell me a story' --wait_for_cr=True" --gpu-enabled\033[0m"
cedana exec -i ced -w /home/ubuntu/nanoGPT "python3 sample.py --init_from=gpt2 --start='tell me a story' --wait_for_cr=True" --gpu-enabled
echo "\033[1mcedana ps\033[0m"
cedana ps
#cedana exec -i ced -w $PWD ./benchmarks/test.sh
#cedana exec -i ced -w $PWD "python3 benchmarks/1gb_pytorch.py"
echo "\033[1msleep 5\033[0m"
sleep 5
echo "\033[1mcedana dump job -d /test --gpu-enabled --stream 32\033[0m"
cedana dump job ced -d /test --gpu-enabled --stream 2 #32
echo "\033[1mcedana ps\033[0m"
cedana ps
echo "\033[1msleep 2\033[0m"
sleep 2
echo "\033[1mcedana restore job ced --stream 32\033[0m"
cedana restore job ced --stream 2 #32
echo "\033[1msleep 2\033[0m"
sleep 2
echo "\033[1mcedana ps\033[0m"
cedana ps
echo "\033[1mps aux | grep gpt2\033[0m"
ps aux | grep gpt2
#echo "\033[1msudo systemctl stop cedana.service\033[0m"
#sudo systemctl stop cedana.service

