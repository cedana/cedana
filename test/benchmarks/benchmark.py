import csv
import os
import shutil
import subprocess
import time
import requests 
import profile_pb2
from google.cloud import bigquery
from google.cloud.bigquery import LoadJobConfig, SourceFormat


import psutil

benchmarking_dir = "benchmarks"
output_dir = "benchmark_results"


def setup():
    # download benchmarking repo
    repo_url="https://github.com/cedana/cedana-benchmarks"
    subprocess.run(["git", "clone", repo_url, benchmarking_dir])

    # make folder for storing results
    os.makedirs(output_dir, exist_ok=True)

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


def start_pprof(filename): 
    pprof_base_url = "http://localhost:6060"
    resp = requests.get(f"{pprof_base_url}/start-profiling?prefix={filename}")
    print("got status code {} from profiler".format(resp.status_code))

def stop_pprof(filename):
    pprof_base_url = "http://localhost:6060"
    resp = requests.get(f"{pprof_base_url}/stop-profiling?filename={filename}")
    print("got status code {} from profiler".format(resp.status_code))
    if resp.status_code != 200:
        print("error from profiler: {}".format(resp.text))

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


def run_checkpoint(daemonPID, jobID, process_pid, iteration, output_dir): 
    chkpt_cmd = "sudo ./cedana dump job {} -d tmp".format(jobID)

    process = get_process_by_pid(process_pid)
    process_mem_used = process.memory_info().rss 

    mem, cpu, disk = start_resource_measurement(daemonPID)
    checkpoint_started_at = time.perf_counter()

    cpu_profile_filename = "{}/cpu_{}_{}".format(output_dir, jobID, iteration)
  
    start_pprof(cpu_profile_filename) 
    p = subprocess.Popen(["sh", "-c", chkpt_cmd], stdout=subprocess.PIPE)
    # used for capturing full time instead of directly exiting
    p.wait()
    checkpoint_completed_at = time.perf_counter()
    stop_pprof(cpu_profile_filename)

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

def analyze_pprof(output_dir):
    pass 

def push_to_bigquery():
# Initialize a BigQuery client
    client = bigquery.Client()

    dataset_id = 'devtest'
    table_id = 'benchmarking_naiive'

# Set the path to your CSV file
    csv_file_path = 'benchmark_output.csv'

# Create a job config
    job_config = LoadJobConfig(
        source_format=SourceFormat.CSV,
        skip_leading_rows=1,  # Change this according to your CSV file
        autodetect=True,  # Auto-detect schema if the table doesn't exist
        write_disposition="WRITE_TRUNCATE",  # Options are WRITE_APPEND, WRITE_EMPTY, WRITE_TRUNCATE
)

# Get the dataset and table reference
    dataset_ref = client.dataset(dataset_id)
    table_ref = dataset_ref.table(table_id)

    # API request to start the job
    with open(csv_file_path, "rb") as source_file:
        load_job = client.load_table_from_file(
            source_file,
            table_ref,
            job_config=job_config
        )  

    load_job.result()  

    if load_job.errors is not None:
        print('Errors:', load_job.errors)
    else:
        print('Job finished successfully.')

    # Get the table details
    table = client.get_table(table_ref)  
    print('Loaded {} rows to {}'.format(table.num_rows, table_id))
  
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
    num_samples = 20
    for x in range(len(jobIDs)): 
        jobID = jobIDs[x]
        for y in range(num_samples):
            process_pid = run_exec(cmds[x], jobID)
            time.sleep(1)
            run_checkpoint(daemon_pid, jobID, process_pid, y, output_dir)
            time.sleep(1)

    # todo - run analysis on pprof and make new csv 

    push_to_bigquery()

    # delete benchmarking folder
    cleanup()
