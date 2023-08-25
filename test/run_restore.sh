rm benchmarking/pids/*

sudo /usr/local/go/bin/go test -count=1 -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkRestore$ github.com/cedana/cedana/cmd && \
