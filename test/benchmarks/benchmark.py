import asyncio
import cedana_bindings as cedana
import time

async def main(daemon_pid, dump_type):
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
        "'code-server --bind-addr localhost:1234'",
        "'python3 regression/main.py'",
        "'python3 1gb_pytorch.py'",
    ]

    # run in a loop
    num_samples = 5
    for x in range(len(jobs)):
        print("Starting benchmarks for job {} with command {}".format(jobs[x], cmds[x]))
        job = jobs[x]
        for y in range(num_samples):
            jobID = job + "-" + str(y)
            process_stats = cedana.run_exec(cmds[x], jobID)
            # wait a few seconds for memory to allocate
            time.sleep(5)

            # we don't mutate jobID for checkpoint/restore here so we can pass the unadulterated one to our csv
            await cedana.run_checkpoint(daemon_pid, jobID, cedana.output_dir, process_stats, dump_type)
            time.sleep(3)

            cedana.run_restore(daemon_pid, jobID, cedana.output_dir)
            time.sleep(3)

            cedana.terminate_process(process_stats["pid"])

    # unique uuid for blob id
    return "benchmark-data-" + str(time.time())

if __name__ == "__main__":
    asyncio.run(main(sys.argv))
