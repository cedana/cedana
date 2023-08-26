#!/bin/bash

# Set the path to your execLoop and dirPids
execLoop="benchmarking/processes/loop2"
execServer="benchmarking/processes/server2"

dirPids="benchmarking/pids"
dirResults="benchmarking/results"

dirResults="benchmarking/results"
dirTempPytorchVision="benchmarking/temp/pytorch-vision"

sudo rm -rf benchmarking/temp/loop/*
sudo rm -rf benchmarking/temp/server/*
sudo rm -rf benchmarking/temp/pytorch/_usr*
sudo rm -rf benchmarking/temp/pytorch-regression/_usr*
sudo rm -rf benchmarking/temp/pytorch-vision/_usr*

# Check if the execLoop exists
if [ ! -f "$execLoop" ]; then
    echo "Loop program not found: $execLoop"
    exit 1
fi

# Check if the dirPids exists
if [ ! -d "$dirPids" ]; then
    echo "benchmarking/pids not found: $dirPids"
    exit 1
fi

# Remove all files in the dirPids
rm -f "$dirResults"/*
# rm -f "$dirPids"/*
echo "All files in the benchmarking/pids directory have been removed."

# Run the execLoop in the background
setsid --fork benchmarking/processes/loop2 < /dev/null &> /dev/null &

# Run tests
sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpLoop$ github.com/cedana/cedana/test/benchmarks && \

# Remove all files in the dirPids
# rm -f "$dirPids"/*
rm -f "$dirResults"/*
echo "All files in the benchmarking/pids directory have been removed."

# Check if the execServer exists
if [ ! -f "$execServer" ]; then
    echo "Server program not found: $execServer"
    exit 1
fi
# Run the execServer in the background
setsid --fork benchmarking/processes/server2 < /dev/null &> /dev/null &
sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpServer$ github.com/cedana/cedana/cmd && \

Remove all files in the dirPids
rm -f "$dirPids"/*
rm -f "$dirResults"/*
echo "All files in the benchmarking/pids directory have been removed."

python3 benchmarking/processes/time_sequence_prediction2/generate_sine_wave.py && \
setsid --fork python3 benchmarking/processes/time_sequence_prediction2/train.py < /dev/null &> /dev/null & \

sleep 15 && \

sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpPytorch$ github.com/cedana/cedana/test/benchmarks && \


sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkRestore$ github.com/cedana/cedana/cmd && \
rm -f "$dirResults"/*

setsid --fork python3 benchmarking/processes/super_resolution2/main.py --upscale_factor 3 --batchSize 4 --testBatchSize 100 --nEpochs 60 --lr 0.001 < /dev/null &> /dev/null & \

sleep 5 && \

sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpPytorchVision$ github.com/cedana/cedana/test/benchmarks && \

rm -f "$dirPids"/*
rm -f "$dirResults"/*
echo "All files in the benchmarking/pids directory have been removed."

setsid --fork python3 benchmarking/processes/regression2/main.py < /dev/null &> /dev/null &

sleep 5 && \

sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpPytorchRegression$ github.com/cedana/cedana/test/benchmarks && \

sudo rm -rf benchmarking/temp/loop/*
sudo rm -rf benchmarking/temp/server/*
sudo rm -rf benchmarking/temp/pytorch/_usr*
sudo rm -rf benchmarking/temp/pytorch-regression/_usr*
sudo rm -rf benchmarking/temp/pytorch-vision/_usr*
