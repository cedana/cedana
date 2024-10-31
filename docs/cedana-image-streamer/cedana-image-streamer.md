## Checkpoint/Restore with cedana-image-streamer
> [!WARNING]
> This feature is still experimental.

This doc describes the process to save-migrate-resume any workload with cedana-image-streamer. It includes building `cedana-image-streamer` and a `cedana-image-streamer`-compatible CRIU.

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
# if GPU: CEDANA_GPU_ENABLED=true
sudo ./reset.sh --systemctl
./build-start-daemon.sh --systemctl
```
6. Run workload as normal
```
cedana exec -i job_id -w working_dir "job-cmd" # if GPU: --gpu-enabled
```
7. Checkpoint workload, providing the level of parallelism (number of threads/pipes). 
```
cedana dump job job_id -d dump_dir --stream num
```
8. Restore workload with same `num`.
```
cedana restore job job_id --stream num
```

### Examples

Simple neural network [[source](https://github.com/cedana/cedana-benchmarking)]
<pre>
<b>sudo rm -rf /test</b>
<b>sudo rm /var/log/cedana*</b>
<b>sudo ./reset.sh --systemctl</b>
Using systemctl
/usr/bin/sudo
<b>./build-start-daemon.sh --systemctl</b>
Using systemctl
Building cedana...
Build successful. Copying the cedana binary
Restarting cedana...
Creating /etc/systemd/system/cedana.service...
Reloading systemd...
Enabling and starting cedana service...
cedana service setup complete.
<b>sleep 1</b>
<b>cedana exec -i ced -w /home/ubuntu/cedana-benchmarking "python3 benchmarks/1gb_pytorch.py"</b>
9:29AM INF Task started: Message:"Job started successfully"  PID:233597  JID:"ced"
<b>cedana ps</b>
+--------+---------+--------+-------------+------------+-------+
| JOB ID |  TYPE   |  PID   |   STATUS    | CHECKPOINT | GPU?  |
+--------+---------+--------+-------------+------------+-------+
| ced    | process | 233597 | JOB_RUNNING |            | false |
+--------+---------+--------+-------------+------------+-------+
<b>sleep 45</b>
<b>cedana dump job -d /test --stream 8</b>
9:29AM INF Success Checkpoint=/test stats={"CRIUDuration":598,"CheckpointFileStats":{"Duration":3,"Size":504211735},"PrepareDuration":13}
<b>cedana ps</b>
+--------+---------+--------+------------+------------+-------+
| JOB ID |  TYPE   |  PID   |   STATUS   | CHECKPOINT | GPU?  |
+--------+---------+--------+------------+------------+-------+
| ced    | process | 233597 | JOB_KILLED | /test      | false |
+--------+---------+--------+------------+------------+-------+
<b>ls -l /test</b>
total 492900
srwxrwxrwx 1 root root        0 Oct 31 09:29 ced-capture.sock
-rw------- 1 root root   498736 Oct 31 09:29 cedana-dump.log
srwxrwxrwx 1 root root        0 Oct 31 09:29 gpu-capture.sock
-rw-r--r-- 1 root root 62788225 Oct 31 09:29 img-0.lz4
-rw-r--r-- 1 root root 60701866 Oct 31 09:29 img-1.lz4
-rw-r--r-- 1 root root 62405437 Oct 31 09:29 img-2.lz4
-rw-r--r-- 1 root root 63146203 Oct 31 09:29 img-3.lz4
-rw-r--r-- 1 root root 62721954 Oct 31 09:29 img-4.lz4
-rw-r--r-- 1 root root 63153376 Oct 31 09:29 img-5.lz4
-rw-r--r-- 1 root root 64686759 Oct 31 09:29 img-6.lz4
-rw-r--r-- 1 root root 64607915 Oct 31 09:29 img-7.lz4
-rw-r--r-- 1 root root       53 Oct 31 09:29 stats-dump
srwxrwxrwx 1 root root        0 Oct 31 09:29 streamer-capture.sock
<b>cedana restore job ced --stream 8</b>
9:29AM INF Success PID=233597 stats={"CRIUDuration":431,"PrepareDuration":496}
<b>sleep 2</b>
<b>cedana ps</b>
+--------+---------+--------+-------------+------------+-------+
| JOB ID |  TYPE   |  PID   |   STATUS    | CHECKPOINT | GPU?  |
+--------+---------+--------+-------------+------------+-------+
| ced    | process | 233597 | JOB_RUNNING | /test      | false |
+--------+---------+--------+-------------+------------+-------+
<b>ps aux | grep pytorch</b>
ubuntu    233597 82.3  4.0 3712496 660628 ?      Ssl  09:29   0:02 python3 benchmarks/1gb_pytorch.py
ubuntu    233745  0.0  0.0   7008  2304 pts/0    S+   09:29   0:00 grep pytorch
</pre>

GPT2-124M inference job [[source](https://github.com/cedana/nanogpt)]
<pre>
<b>sudo rm -rf /test</b>
<b>sudo rm /var/log/cedana*</b>
<b>CEDANA_GPU_ENABLED=true</b>
<b>sudo ./reset.sh --systemctl</b>
Using systemctl
/usr/bin/sudo
<b>./build-start-daemon.sh --systemctl</b>
Using systemctl
Building cedana...
Build successful. Copying the cedana binary
Starting daemon with GPU support...
Restarting cedana...
Creating /etc/systemd/system/cedana.service...
Reloading systemd...
Enabling and starting cedana service...
cedana service setup complete.
<b>sleep 1</b>
<b>cedana exec -i ced -w /home/ubuntu/nanoGPT python3 sample.py --init_from=gpt2 --start=tell me a story --wait_for_cr=True --gpu-enabled</b>
9:37AM INF Task started: Message:"Job started successfully"  PID:234878  JID:"ced"
<b>cedana ps</b>
+--------+---------+--------+-------------+------------+------+
| JOB ID |  TYPE   |  PID   |   STATUS    | CHECKPOINT | GPU? |
+--------+---------+--------+-------------+------------+------+
| ced    | process | 234878 | JOB_RUNNING |            | true |
+--------+---------+--------+-------------+------------+------+
<b>sleep 150</b>
<b>cedana dump job -d /test --stream 4</b>
9:39AM INF Success Checkpoint=/test stats={"CRIUDuration":645,"CheckpointFileStats":{"Duration":4,"Size":1032645332},"GPUDuration":1996,"PrepareDuration":13}
<b>cedana ps</b>
+--------+---------+--------+------------+------------+------+
| JOB ID |  TYPE   |  PID   |   STATUS   | CHECKPOINT | GPU? |
+--------+---------+--------+------------+------------+------+
| ced    | process | 234878 | JOB_KILLED | /test      | true |
+--------+---------+--------+------------+------------+------+
<b>ls -l /test</b>
total 1008984
srwxrwxrwx 1 root root         0 Oct 31 09:39 ced-capture.sock
-rw------- 1 root root    540880 Oct 31 09:39 cedana-dump.log
srwxrwxrwx 1 root root         0 Oct 31 09:39 gpu-capture.sock
-rw-r--r-- 1 root root 237313142 Oct 31 09:39 img-0.lz4
-rw-r--r-- 1 root root 292425693 Oct 31 09:39 img-1.lz4
-rw-r--r-- 1 root root 239288581 Oct 31 09:39 img-2.lz4
-rw-r--r-- 1 root root 263617916 Oct 31 09:39 img-3.lz4
-rw-r--r-- 1 root root        53 Oct 31 09:39 stats-dump
srwxrwxrwx 1 root root         0 Oct 31 09:39 streamer-capture.sock
<b>cedana restore job ced --stream 4</b>
9:39AM INF Success PID=234878 stats={"CRIUDuration":9128,"GPUDuration":8810,"GPURestoreStats":{"copyMemTime":240,"replayCallsTime":7565},"PrepareDuration":1497}
<b>sleep 2</b>
<b>cedana ps</b>
+--------+---------+--------+-------------+------------+------+
| JOB ID |  TYPE   |  PID   |   STATUS    | CHECKPOINT | GPU? |
+--------+---------+--------+-------------+------------+------+
| ced    | process | 234878 | JOB_RUNNING | /test      | true |
+--------+---------+--------+-------------+------------+------+
<b>ps aux | grep gpt2</b>
ubuntu    234878 33.7  2.8 12701440 468460 ?     Rsl  09:39   0:03 python3 sample.py --init_from=gpt2 --start=tell me a story --wait_for_cr=True
ubuntu    235165  0.0  0.0   7008  2304 pts/0    S+   09:39   0:00 grep gpt2
</pre>
