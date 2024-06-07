import asyncio
import benchmark
import continuous
import correctness
import os
import process_benchmarks
import psutil
import shutil
import smoke
import subprocess
import sys
from tplib import task_pb2

benchmarking_dir = "benchmarks"
output_dir = "benchmark_results"


def get_pid_by_name(process_name: str) -> int:
    for proc in psutil.process_iter(["name"]):
        if proc.info["name"] == process_name:
            return proc.pid
    return -1


def setup() -> int:
    # download benchmarking repo
    repo_url = "https://github.com/cedana/cedana-benchmarks"
    subprocess.run(["git", "clone", repo_url, benchmarking_dir])

    # make folder for storing results
    os.makedirs(output_dir, exist_ok=True)

    return get_pid_by_name("cedana")


def cleanup():
    shutil.rmtree(benchmarking_dir)


async def main(args):
    daemon_pid = setup()
    if daemon_pid == -1:
        print("ERROR: cedana process not found in active PIDs. Have you started cedana daemon?")
        sys.exit(1)

    remote = 0 if "--local" in args or "--smoke" in args else 1
    num_samples = (5 if "--num_samples" not in args else int(args[args.index("--num_samples") + 1]))
    verbose = True if "--verbose" in args else False

    if "--correctness" in args:
        blob_id = await correctness.main(daemon_pid, remote, verbose)
    elif "--smoke" in args:
        blob_id = await smoke.main(daemon_pid, remote, num_samples=num_samples)
    elif "--continuous" in args:
        name = next((arg.split('=')[1] for arg in sys.argv if arg.startswith('--name=')), 'base')
        blob_id = await continuous.main(daemon_pid, remote, name)
    else:
        blob_id = await benchmark.main(daemon_pid, remote, num_samples=num_samples)
        process_benchmarks.main(remote, blob_id)

    # delete benchmarking folder
    cleanup()


if __name__ == "__main__":
    asyncio.run(main(sys.argv))
