import cedana_bindings as cedana
import psutil
import time

async def main(daemon_pid, dump_type):
    job = "nn-1gb"
    cmd = "'python3 1gb_pytorch_correctness.py'"
    print("Starting correctness test for job {} with command {}".format(job, cmd))
    jobID_base = job + "-base"
    jobID_saved = job + "-saved"
    process_stats_base = cedana.run_exec("'python3 1gb_pytorch_correctness.py nn-1gb-base'", jobID_base)
    process_stats_saved = cedana.run_exec("'python3 1gb_pytorch_correctness.py nn-1gb-saved'", jobID_saved)
    #time.sleep(5)
    #await cedana.run_checkpoint(daemon_pid, jobID_saved, cedana.output_dir, process_stats_saved, dump_type)
    #time.sleep(3) # vary time and  check checkpoints
    #cedana.run_restore(daemon_pid, jobID_saved, cedana.output_dir)
    #time.sleep(3)
    print("process_stats_base:\n", process_stats_base)
    print("process_stats_saved:\n", process_stats_saved)
    # do not terminate, wait for the jobs to exit and then compare checkpoints
    print("psutil.process_iter([\"pid\"]) = \n",psutil.pids())
    while process_stats_base["pid"] in psutil.pids() or process_stats_saved["pid"] in psutil.pids():
        continue
    print("[PROCESS\u001b[35m", jobID_base, "\033[0mAND PROCESS\u001b[35m", jobID_saved, "\033[0mDONE.]")

    return "correctness-data-" + str(time.time())
