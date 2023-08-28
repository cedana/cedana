#!/bin/bash

execLoop="benchmarking/processes/loop"
execServer="benchmarking/processes/server"

dirPids="benchmarking/pids"
dirResults="benchmarking/results"

dirTempPytorchVision="benchmarking/temp/pytorch-vision"

sudo rm -rf benchmarking/temp/loop/*
sudo rm -rf benchmarking/temp/server/*
sudo rm -rf benchmarking/temp/pytorch/_usr*
sudo rm -rf benchmarking/temp/pytorch-regression/_usr*
sudo rm -rf benchmarking/temp/pytorch-vision/_usr*

if [ ! -f "$execLoop" ]; then
    echo "Loop program not found: $execLoop"
    exit 1
fi

if [ ! -d "$dirPids" ]; then
    echo "benchmarking/pids not found: $dirPids"
    exit 1
fi

if [ ! -f "$execServer" ]; then
    echo "Server program not found: $execServer"
    exit 1
fi


rm -f "$dirResults"/*
rm -f "$dirPids"/*
setsid --fork benchmarking/processes/loop < /dev/null &> /dev/null &

sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpLoop$ github.com/cedana/cedana/test/benchmarks && \
sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkLoopRestore$ github.com/cedana/cedana/test/benchmarks && \

rm -f "$dirResults"/*
rm -f "$dirPids"/*

setsid --fork benchmarking/processes/server < /dev/null &> /dev/null &
sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpServer$ github.com/cedana/cedana/test/benchmarks && \
sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkServerRestore$ github.com/cedana/cedana/test/benchmarks && \

rm -f "$dirPids"/*
rm -f "$dirResults"/*

python3 benchmarking/processes/time_sequence_prediction/generate_sine_wave.py && \
setsid --fork python3 benchmarking/processes/time_sequence_prediction/train.py < /dev/null &> /dev/null & \

sleep 15 && \

sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpPytorch$ github.com/cedana/cedana/test/benchmarks && \
sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkPytorchRestore$ github.com/cedana/cedana/test/benchmarks && \

rm -f "$dirPids"/*
rm -f "$dirResults"/*

setsid --fork python3 benchmarking/processes/super_resolution/main.py --upscale_factor 3 --batchSize 4 --testBatchSize 100 --nEpochs 60 --lr 0.001 < /dev/null &> /dev/null & \

sleep 5 && \

sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpPytorchVision$ github.com/cedana/cedana/test/benchmarks && \
sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkVisionRestore$ github.com/cedana/cedana/test/benchmarks && \

rm -f "$dirPids"/*
rm -f "$dirResults"/*

setsid --fork python3 benchmarking/processes/regression/main.py < /dev/null &> /dev/null &

sleep 5 && \

sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDumpPytorchRegression$ github.com/cedana/cedana/test/benchmarks && \
sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkRegressionRestore$ github.com/cedana/cedana/test/benchmarks && \

sudo rm -rf benchmarking/temp/loop/*
sudo rm -rf benchmarking/temp/server/*
sudo rm -rf benchmarking/temp/pytorch/_usr*
sudo rm -rf benchmarking/temp/pytorch-regression/_usr*
sudo rm -rf benchmarking/temp/pytorch-vision/_usr*
