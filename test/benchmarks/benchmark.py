import csv
import os
import shutil
import subprocess
import time
import requests 

import psutil

benchmarking_dir = "benchmarks"


def setup():
    # download benchmarking repo
    repo_url="https://github.com/cedana/cedana-benchmarks"
    subprocess.run(["git", "clone", repo_url, benchmarking_dir])

    # get cedana daemon pid from pid file 
    with open("/var/log/cedana.pid", "r") as file:
        daemon_pid = int(file.read().strip())

    print("found daemon running at pid {}".format(daemon_pid))

    return daemon_pid

def cleanup():
    shutil.rmtree(benchmarking_dir)

def get_process_by_pid(pid):
    # Get a psutil.Process object for the given pid
    try:
        return psutil.Process(pid)
    except psutil.NoSuchProcess:
        print("No process found with PID", pid)
        return None

def measure_disk_usage(checkpoint_dir):
    return sum(os.path.getsize(os.path.join(dirpath, filename)) for dirpath, dirnames, filenames in os.walk(checkpoint_dir) for filename in filenames)


def start_resource_measurement(pid):
    process = get_process_by_pid(pid)
    if process is None:
        return None, None

    mem_before = process.memory_info().rss  # Resident Set Size
    cpu_before = process.cpu_percent(interval=1)
    disk_before = psutil.disk_usage('/').used
    return mem_before, cpu_before, disk_before


def start_pprof(jobid, iteration, out_dir): 
    pprof_base_url = "http://localhost:6060"
    cpu_profile_filename = "{}/cpu_{}_{}.pprof".format(out_dir, jobid, iteration)
    requests.get(f"{pprof_base_url}/start-profiling?prefix={cpu_profile_filename}")

def stop_pprof():
    pprof_base_url = "http://localhost:6060"
    requests.get(f"{pprof_base_url}/stop-profiling")

def stop_resource_measurement(pid, mem_before, cpu_before, disk_before, started_at, completed_at, jobID, process_mem_used):
    daemon = get_process_by_pid(pid)
    if daemon is None:
        return None, None

    mem_after = daemon.memory_info().rss
    cpu_after = daemon.cpu_percent(interval=1)
    disk_after = psutil.disk_usage('/').used

    mem_used = mem_after - mem_before
    cpu_used = cpu_after - cpu_before
    disk_used = disk_after - disk_before


    # open and write to a csv 
    with open("benchmark_output.csv", mode='a', newline='') as file:
        writer = csv.writer(file)
        # Write the headers if the file is new
        if file.tell() == 0:
            writer.writerow(['Timestamp', 'Job ID', 'Process Memory', 'Memory Used', 'Disk Used', 'CPU Used (Percent)', 'Time Taken'])
        
        # Write the resource usage data
        writer.writerow([
            time.strftime("%Y-%m-%d %H:%M:%S", time.localtime(time.time())),
            jobID,
            process_mem_used, 
            mem_used,
            disk_used,
            cpu_used,
            completed_at - started_at
        ])


def run_checkpoint(daemonPID, jobID, process_pid, iteration): 
    chkpt_cmd = "sudo ./cedana dump job {} -d tmp".format(jobID)

    process = get_process_by_pid(process_pid)
    process_mem_used = process.memory_info().rss 

    mem, cpu, disk = start_resource_measurement(daemonPID)
    checkpoint_started_at = time.perf_counter()

    start_pprof(jobID, iteration, "benchmark_output") 
    p = subprocess.Popen(["sh", "-c", chkpt_cmd], stdout=subprocess.PIPE)
    # used for capturing full time instead of directly exiting
    p.wait()
    checkpoint_completed_at = time.perf_counter()

    # also writes to a csv 
    stop_resource_measurement(
        daemonPID, 
        mem, 
        cpu, 
        disk, 
        checkpoint_started_at, 
        checkpoint_completed_at, 
        jobID, 
        process_mem_used
        )

def run_restore(jobID):
    restore_started_at = time.perf_counter()
    print("starting restore of process at {}".format(restore_started_at))
    restore_cmd = "sudo ./cedana restore job {}".format(jobID)
    
    p =subprocess.Popen(["sh", "-c", restore_cmd], stdout=subprocess.PIPE)
    p.wait()

    restore_completed_at = time.perf_counter()
    print("completed restore of process at {}".format(restore_completed_at))

def run_exec(cmd, jobID): 
    exec_cmd = "./cedana exec {} {}".format(cmd, jobID)

    process = subprocess.Popen(["sh", "-c", exec_cmd], stdout=subprocess.PIPE)
    pid = int(process.communicate()[0].decode().strip())
    return pid 


def main(): 
    daemon_pid = setup()
    jobIDs = [
        "loop",
        "regression",
    ]
    cmds = [
        "./benchmarks/test.sh",
        "'python3 benchmarks/regression/main.py'"
    ]

    # run in a loop 
    num_samples = 5
    for x in range(len(jobIDs)): 
        jobID = jobIDs[x]
        for y in range(num_samples):
            process_pid = run_exec(cmds[x], jobID)
            time.sleep(1)
            run_checkpoint(daemon_pid, jobID, process_pid, y)
            time.sleep(1)

    # delete benchmarking folder
    cleanup()

main()