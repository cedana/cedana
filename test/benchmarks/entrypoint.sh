#!/bin/bash 

# entrypoint for benchmarking docker script 

# parse args
is_local=false
while [[ $# -gt 0 ]]; do
  case "$1" in 
    --local)
      is_local=true
      shift
      ;;
    *)
      echo "Unknown arg: $1"
      exit 1
      ;;
  esac
done

if [ "$is_local" = true ]; then
  otelcol-contrib --config test/benchmarks/local-otelcol-config.yaml &
  ./reset.sh
  ./build-start-daemon.sh
  sudo -E python3 test/benchmarks/benchmark.py --local
else
  # start otelcol polling 
  otelcol-contrib --config test/benchmarks/otelcol-config.yaml &

  # start daemon 
  sudo -E ./cedana daemon start & 

  # start benchmarking
  sudo -E python3 test/benchmarks/benchmark.py
fi
