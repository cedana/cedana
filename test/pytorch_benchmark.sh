dirPids="benchmarking/pids"
dirResults="benchmarking/results"
dirTempPytorchVision="benchmarking/temp/pytorch-vision"


rm -f "$dirPids"/*
rm -f "$dirResults"/*
echo "All files in the benchmarking/pids directory have been removed."

python3 benchmarking/processes/super_resolution/main.py --upscale_factor 3 --batchSize 4 --testBatchSize 100 --nEpochs 60 --lr 0.001 & \

sleep 5 && \

sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpPytorchVision$ github.com/cedana/cedana/cmd && \

sudo rm -rf "$dirTempPytorchVision"/_usr*

rm -f "$dirPids"/*
rm -f "$dirResults"/*
echo "All files in the benchmarking/pids directory have been removed."

python3 benchmarking/processes/regression/main.py &

sleep 5 && \

sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpPytorchRegression$ github.com/cedana/cedana/cmd

sudo rm -rf "$dirTempPytorchVision"/_usr*


# Dump fails on this program for some reason
# # Check if the execServer exists
# if [ ! -f "$execPing" ]; then
#     echo "Server program not found: $execPing"
#     exit 1
# fi
# # Run the execServer in the background
# "$execPing" &
# sudo /usr/local/go/bin/go test -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/server_memory.prof.gz -run=^$ -bench ^BenchmarkDumpPing$ github.com/cedana/cedana/cmd
