sudo /usr/local/go/bin/go test -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDump$ -parallel 4 github.com/nravic/cedana/cmd
