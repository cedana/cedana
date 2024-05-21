import asyncio
import cedana_bindings as cedana
import time
from tplib import task_pb2


async def main(daemon_pid, remote):

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

    # run in a loop
    num_samples = 5
    for x in range(len(jobs)):
        print(
            "Starting benchmarks for job \033[1m{}\033[0m with command \033[1m{}\033[0m".format(
                jobs[x], cmds[x]
            )
        )
        job = jobs[x]
        for y in range(num_samples):
            jobID = job + "-" + str(y)
            process_stats = await cedana.run_exec(cmds[x], jobID)
            # wait a few seconds for memory to allocate
            time.sleep(5)

            # we don't mutate jobID for checkpoint/restore here so we can pass the unadulterated one to our csv
            dump_resp = await cedana.run_checkpoint(
                daemon_pid, jobID, cedana.output_dir, process_stats, remote
            )
            time.sleep(5)

            restore_resp = await cedana.run_restore(
                daemon_pid, jobID, dump_resp.CheckpointID, cedana.output_dir, remote
            )
            time.sleep(5)

            cedana.terminate_process(process_stats["pid"])

    # unique uuid for blob id
    return "benchmark-data-" + str(time.time())


if __name__ == "__main__":
    asyncio.run(main(sys.argv))
