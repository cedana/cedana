# Set the path to your executable
EXECUTABLE="benchmarking/processes/loop"
DIRECTORY="benchmarking/pids"

# Check if the executable exists
if [ -f "$EXECUTABLE" ]; then

    if [ -d "$DIRECTORY" ]; then
        # Remove all files in the directory
        rm -f "$DIRECTORY"/*
        echo "All files in the directory have been removed."

        "$EXECUTABLE" &

        sudo /usr/local/go/bin/go test -cpuprofile benchmarking/results/cpu.prof.gz -memprofile benchmarking/results/memory.prof.gz -run=^$ -bench ^BenchmarkDump$ -parallel 4 github.com/nravic/cedana/cmd
    else
        echo "Directory not found: $DIRECTORY"
    fi
    # Execute the executable

else
    echo "Executable not found: $EXECUTABLE"
    exit 1
fi
