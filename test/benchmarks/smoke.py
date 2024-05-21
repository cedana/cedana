import random
import asyncio
import cedana_bindings as cedana
import time
from tplib import task_pb2
import subprocess


async def main(daemon_pid, remote, num_samples=5):
    print("running adjust pid script...")
    try:
        result = subprocess.run(
            ["/bin/bash", "adjust_pids.sh"], check=True, text=True, capture_output=True
        )
        print("Script output:", result.stdout)
    except subprocess.CalledProcessError as e:
        print("Error running script:", e.stderr)

    print("Starting smoke test with {} samples".format(num_samples))
    jobs = [
        "server",
        "loop",
        "vscode-server",
        "regression",
        "nn-1gb",
    ]
    cmds = [
        "./server",
        "./test.sh",
        "code-server --bind-addr localhost:1234",
        "python3 regression/main.py",
        "python3 1gb_pytorch.py",
    ]

    # pick a random job and run it
    for _ in range(num_samples):
        index = random.randint(0, len(jobs) - 1)
        job = jobs[index]
        cmd = cmds[index]
        print(
            "Starting smoke tests for job \033[1m{}\033[0m with command \033[1m{}\033[0m".format(
                job, cmd
            )
        )
        jobID = job + "-" + str(_)
        process_stats = await cedana.run_exec(cmd, jobID)
        # wait a few seconds for memory to allocate
        time.sleep(5)

        # we don't mutate jobID for checkpoint/restore here so we can pass the unadulterated one to our csv
        dump_resp = await cedana.run_checkpoint(
            daemon_pid, jobID, cedana.output_dir, process_stats, remote
        )
        time.sleep(3)

        restore_resp = await cedana.run_restore(
            daemon_pid, jobID, dump_resp.CheckpointID, cedana.output_dir, remote
        )
        time.sleep(3)

        cedana.terminate_process(process_stats["pid"])

    # unique uuid for blob id
    return "smoke-data-" + str(time.time())


if __name__ == "__main__":
    asyncio.run(main(sys.argv))
