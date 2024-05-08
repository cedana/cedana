import cedana_bindings as cedana
import psutil
import time
from tplib import task_pb2

async def main(daemon_pid, remote):
    job = "nn-1gb"
    cmd = "python3 1gb_pytorch_correctness.py"
    print("Starting correctness test for job \033[1m{}\033[0m with command \033[1m{}\033[0m".format(job, cmd))
    jobID_base = job + "-base"
    jobID_saved = job + "-saved"
    process_stats_base = await cedana.run_exec(cmd+" "+jobID_base, jobID_base)
    process_stats_saved = await cedana.run_exec(cmd+" "+jobID_saved, jobID_saved)
    #time.sleep(5)
    #await cedana.run_checkpoint(daemon_pid, jobID_saved, cedana.output_dir, process_stats_saved, remote)
    #time.sleep(3) # vary time and check checkpoints
    #await cedana.run_restore(daemon_pid, jobID_saved, cedana.output_dir, remote)
    #time.sleep(3)
    print("process_stats_base:\n", process_stats_base)
    print("process_stats_saved:\n", process_stats_saved)
    # do not terminate, wait for the jobs to exit and then compare checkpoints
    print("psutil.process_iter([\"pid\"]) = \n",psutil.pids())
    while process_stats_base["pid"] in psutil.pids() or process_stats_saved["pid"] in psutil.pids():
        continue
    print("[PROCESS\u001b[35m", jobID_base, "\033[0mAND PROCESS\u001b[35m", jobID_saved, "\033[0mDONE.]")

    return "correctness-data-" + str(time.time())
