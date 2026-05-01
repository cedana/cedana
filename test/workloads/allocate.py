import argparse
import os
import time

DEFAULT_RAM_MB = 400

def hybrid_load(shared_size): # Allocate a large bytearray (this takes up the RAM)
    data = bytearray(shared_size)
    print(f"Process {os.getpid()} allocated {shared_size / 1024**2:.2f} MB")

    while True:
        # This forces the CPU to constantly fetch from RAM. We use a slice and a simple sum to keep the CPU pinned.
        _ = sum(data[::1000])

def parse_args():
    parser = argparse.ArgumentParser(description="Memory stress workload.")
    parser.add_argument(
        "ram_mb",
        type=int,
        nargs="?",
        default=DEFAULT_RAM_MB,
        help="RAM to allocate in MB (default: %(default)s)",
    )
    return parser.parse_args()

if __name__ == "__main__":
    args = parse_args()
    if args.ram_mb <= 0:
        raise SystemExit("ram_mb must be a positive integer")
    bytes_per_core = int(args.ram_mb * 1024**2)
    print("Press Ctrl+C to stop.")
    try:
        hybrid_load(bytes_per_core)
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        print("\nStopping workload...")
