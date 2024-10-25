## Checkpoint/Restore with cedana-image-streamer
> [!WARNING]
> This feature is still experimental.

This doc describes the process to save-migrate-resume a workload with cedana-image-streamer. It includes building `cedana-image-streamer` and a `cedana-image-streamer`-compatible CRIU. 

1. Clone [`cedana-image-streamer`](https://github.com/cedana/cedana-image-streamer)
2. Build and add to PATH
```
cargo build --release --bin cedana-image-streamer
sudo cp target/release/cedana-image-streamer /usr/bin
```
3. Clone [this fork of CRIU](https://github.com/lianakoleva/criu_), which supports enabling CRIU image streaming through gRPC. 
4. Build and add to PATH
```
make criu
sudo make install-criu
```
5. Reset and build `cedana` as normal
```
sudo ./reset.sh --systemctl
./build-start-daemon.sh --systemctl --gpu-enabled
```
6. Run workload as normal
```
cedana exec -i job_id -w working_dir "job-cmd" [--gpu-enabled]
```
7. Checkpoint workload, providing the level of parallelism (number of threads/pipes). 
```
cedana dump job job_id -d dump_dir --stream num
```
8. Restore workload with same `num`.
```
cedana restore job job_id --stream num
```

### Example
```
sudo ./reset.sh --systemctl
./build-start-daemon.sh --systemctl --gpu-enabled
sleep 1
cedana exec -i ced -w /home/ubuntu/nanoGPT "python3 sample.py --init_from=gpt2 --start='tell me a story' --wait_for_cr=True" --gpu-enabled
cedana ps
sleep 45
cedana dump job ced -d /test --stream 4
cedana ps
sleep 2
cedana restore job ced --stream 4
sleep 2
cedana ps
ps aux | grep gpt2
```
