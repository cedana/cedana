echo "\033[1msudo rm -rf /test\033[0m"
sudo rm -rf /test
echo "\033[1msudo rm /var/log/cedana*\033[0m"
sudo rm /var/log/cedana*
echo "\033[1msudo ./reset.sh --systemctl\033[0m"
sudo ./reset.sh --systemctl
echo "\033[1m./build-start-daemon.sh --systemctl\033[0m" #--gpu-enabled
./build-start-daemon.sh --systemctl #--gpu-enabled
# echo "\033[1msleep 2\033[0m"
# sleep 2
#cedana exec -i ced -w $PWD ./benchmarks/test.sh
# echo "\033[1mcedana exec -i ced -w $PWD "python3 benchmarks/1gb_pytorch.py"\033[0m"
# cedana exec -i ced -w $PWD "python3 benchmarks/1gb_pytorch.py"
echo "\033[1maws s3 ls --summarize s3://direct-remoting\033[0m"
aws s3 ls --summarize s3://direct-remoting
echo "\033[1mcedana exec -i ced -w /home/ubuntu/nanoGPT "python3 sample.py --init_from=gpt2 --start='tell me a story' --wait_for_cr=True" --gpu-enabled\033[0m"
cedana exec -i ced -w /home/ubuntu/nanoGPT "python3 sample.py --init_from=gpt2 --start='tell me a story' --wait_for_cr=True" --gpu-enabled
echo "\033[1mcedana ps\033[0m"
cedana ps
echo "\033[1msleep 15\033[0m"
sleep 15
# echo "\033[1mcedana dump job -d /test\033[0m"
echo "\033[1mcedana dump job -d /test --stream 4\033[0m"
cedana dump job ced -d /test --stream 4
echo "\033[1mcedana ps\033[0m"
cedana ps
# echo "\033[1msleep 2\033[0m"
# sleep 2
echo "\033[1mls -l /test\033[0m"
ls -l /test
echo "\033[1maws s3 ls --summarize s3://direct-remoting\033[0m"
aws s3 ls --summarize s3://direct-remoting
# echo "\033[1mcedana restore job ced\033[0m"
# echo "\033[1mcd /test\033[0m" DOESNT WORK
# cd test
# echo "\033[1mls -l /test\033[0m"
# ls -l /test
# echo "\033[1maws s3api get-object --bucket direct-remoting --key img-0.lz4 /test/img-0.lz4\033[0m"
# aws s3api get-object --bucket direct-remoting --key img-0.lz4 /test/img-0.lz4
# echo "\033[1mls -l /test\033[0m"
# ls -l /test
# echo "\033[1maws s3api get-object --bucket direct-remoting --key img-1.lz4 /test/img-1.lz4\033[0m"
# aws s3api get-object --bucket direct-remoting --key img-1.lz4 /test/img-1.lz4
# echo "\033[1mls -l /test\033[0m"
# ls -l /test
# echo "\033[1maws s3api get-object --bucket direct-remoting --key img-2.lz4 /test/img-2.lz4\033[0m"
# aws s3api get-object --bucket direct-remoting --key img-2.lz4 /test/img-2.lz4
# echo "\033[1mls -l /test\033[0m"
# ls -l /test
# echo "\033[1maws s3api get-object --bucket direct-remoting --key img-3.lz4 /test/img-3.lz4\033[0m"
# aws s3api get-object --bucket direct-remoting --key img-3.lz4 /test/img-3.lz4
# echo "\033[1mls -l /test\033[0m"
# ls -l /test
echo "\033[1mcedana restore job ced --stream 4 \033[0m"
cedana restore job ced --stream 4
# echo "\033[1msleep 2\033[0m"
# sleep 2
echo "\033[1mcedana ps\033[0m"
cedana ps
# echo "\033[1mps aux | grep pytorch\033[0m"
# ps aux | grep pytorch
echo "\033[1mps aux | grep gpt2\033[0m"
ps aux | grep gpt2
# echo "\033[1msudo systemctl stop cedana.service\033[0m"
# sudo systemctl stop cedana.service
