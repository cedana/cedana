import csv
import grpc
import os
import platform
import psutil
import signal
import subprocess
from tplib import task_pb2
from tplib import task_pb2_grpc
import time

output_dir = "benchmark_results"

cedana_version = (
    subprocess.check_output(["git", "describe", "--tags"]).decode("utf-8").strip()
)

def start_recording(pid):
    initial_data = {}
    try:
        p = psutil.Process(pid)
        initial_data["cpu_times"] = p.cpu_times()
        initial_data["memory"] = p.memory_full_info().uss
        initial_data["disk_io"] = psutil.disk_io_counters()
        initial_data["cpu_percent"] = p.cpu_percent(interval=None)
        initial_data["time"] = psutil.cpu_times()
    except psutil.NoSuchProcess:
        print(f"No such process with PID {pid}")

    return initial_data

def stop_recording(
    operation_type,
    pid,
    initial_data,
    jobID,
    process_stats,
):
    p = psutil.Process(pid)
    current_cpu_times = p.cpu_times()
    cpu_time_user_diff = current_cpu_times.user - initial_data["cpu_times"].user
    cpu_time_system_diff = current_cpu_times.system - initial_data["cpu_times"].system
    cpu_utilization = cpu_time_user_diff + cpu_time_system_diff

    cpu_time_total_diff = cpu_time_user_diff + cpu_time_system_diff

    # Calculate the total time all CPUs have spent since we started recording
    current_time = psutil.cpu_times()
    cpu_total_time_diff = sum(
        getattr(current_time, attr) - getattr(initial_data["time"], attr)
        for attr in ["user", "system", "idle"]
    )

    # Calculate the percentage of CPU utilization
    cpu_percent = (
        100 * cpu_time_total_diff / cpu_total_time_diff if cpu_total_time_diff else 0
    )

    # Memory usage in KB
    current_memory = p.memory_full_info().uss
    memory_used_kb = (current_memory - initial_data["memory"]) / 1024

    # Disk I/O
    current_disk_io = psutil.disk_io_counters()
    read_count_diff = current_disk_io.read_count - initial_data["disk_io"].read_count
    write_count_diff = current_disk_io.write_count - initial_data["disk_io"].write_count
    read_bytes_diff = current_disk_io.read_bytes - initial_data["disk_io"].read_bytes
    write_bytes_diff = current_disk_io.write_bytes - initial_data["disk_io"].write_bytes

    # Machine specs
    processor = platform.processor()
    physical_cores = psutil.cpu_count(logical=False)
    cpu_count = psutil.cpu_count(logical=True)
    memory = psutil.virtual_memory().total / (1024 ** 3)

    # read from otelcol json
    with open("benchmark_output.csv", mode="a", newline="") as file:
        writer = csv.writer(file)
        # Write the headers if the file is new
        if file.tell() == 0:
            writer.writerow(
                [
                    "Timestamp",
                    "Job ID",
                    "Operation Type",
                    "Memory Used Target (KB)",
                    "Memory Used Daemon",
                    "Write Count",
                    "Read Count",
                    "Write (MB)",
                    "Read Bytes (MB)",
                    "CPU Utilization (Secs)",
                    "CPU Used (Percent)",
                    "Cedana Version",
                    "Processor",
                    "Physical Cores",
                    "CPU Cores",
                    "Memory (GB)",
                    "blob_id",
                ]
            )

        # Write the resource usage data
        writer.writerow(
            [
                time.strftime("%Y-%m-%d %H:%M:%S", time.localtime(time.time())),
                jobID,
                operation_type,
                process_stats["memory_kb"],
                memory_used_kb,
                write_count_diff,
                read_count_diff,
                write_bytes_diff / (1024 * 1024),  # convert to MB
                read_bytes_diff / (1024 * 1024),  # convert to MB
                cpu_utilization,
                cpu_percent,
                cedana_version,
                processor,
                physical_cores,
                cpu_count,
                memory,
                "",
            ]
        )

        # delete profile file after

def terminate_process(pid, timeout=3):
    try:
        # Send SIGTERM
        os.kill(pid, signal.SIGTERM)

        # Wait for the process to terminate
        start_time = time.time()
        while os.path.exists(f"/proc/{pid}") and time.time() - start_time < timeout:
            time.sleep(0.1)  # Sleep briefly to avoid a busy wait

        if os.path.exists(f"/proc/{pid}"):
            # If the process is still alive after the timeout, send SIGKILL
            os.kill(pid, signal.SIGKILL)
            print(f"Process {pid} forcefully terminated.")
        else:
            print(f"Process {pid} terminated gracefully.")
    except ProcessLookupError:
        print(f"Process {pid} does not exist.")
    except PermissionError:
        print(f"No permission to terminate process {pid}.")


async def run_checkpoint(daemonPID, jobID, output_dir, process_stats, dump_type):
    channel = grpc.aio.insecure_channel("localhost:8080")
    dump_args = task_pb2.DumpArgs()
    dump_args.Dir = "/tmp"
    dump_args.Type = dump_type
    dump_args.JID = jobID
    stub = task_pb2_grpc.TaskServiceStub(channel)

    # initial data here is fine - we want to measure impact of daemon on system
    initial_data = start_recording(daemonPID)
    cpu_profile_filename = "{}/cpu_{}_checkpoint".format(output_dir, jobID)

    dump_resp = await stub.Dump(dump_args)

    time.sleep(5)
    stop_recording("checkpoint", daemonPID, initial_data, jobID, process_stats)

    return dump_resp

async def run_restore(
    daemonPID, jobID, checkpointID, output_dir, restore_type, max_retries=2, delay=5
):
    channel = grpc.aio.insecure_channel("localhost:8080")
    restore_args = task_pb2.RestoreArgs()
    restore_args.Type = restore_type
    restore_args.JID = jobID
    if restore_type == task_pb2.CRType.REMOTE:
        restore_args.CheckpointId = checkpointID
    else:
        restore_args.CheckpointPath = checkpointID
    restore_args.UID = os.getuid()
    restore_args.GID = os.getgid()
    stub = task_pb2_grpc.TaskServiceStub(channel)

    initial_data = start_recording(daemonPID)
    cpu_profile_filename = "{}/cpu_{}_restore".format(output_dir, jobID)

    # we add a retrier here because PID conflicts happen due to a race condition inside docker containers
    # this is not an issue outside of docker containers for some reason, TODO NR to investigate further
    for attempt in range(max_retries):
        try:
            restore_resp = await stub.Restore(restore_args)
            break  # Exit the loop if successful
        except grpc.aio.AioRpcError as e:
            if "File exists" in e.details() and attempt < max_retries - 1:
                print("PID conflict detected, retrying...")
                time.sleep(delay)
            else:
                raise

    # nil value here
    process_stats = {}
    process_stats["memory_kb"] = 0

    time.sleep(5)
    stop_recording("restore", daemonPID, initial_data, jobID, process_stats)

    return restore_resp


async def run_exec(cmd, jobID):
    channel = grpc.aio.insecure_channel("localhost:8080")
    start_task_args = task_pb2.StartArgs()
    start_task_args.Task = cmd
    start_task_args.JID = jobID
    start_task_args.WorkingDir = os.path.join(os.getcwd(), "benchmarks")
    env = []  # format to match golang os.Environ()
    for key in os.environ.keys():
        env.append(key+"="+os.environ[key])
    start_task_args.Env.extend(env)
    start_task_args.UID = os.getuid()
    start_task_args.GID = os.getgid()
    stub = task_pb2_grpc.TaskServiceStub(channel)

    start_task_resp = await stub.Start(start_task_args)

    process_stats = {}
    process_stats["pid"] = start_task_resp.PID
    psutil_process = psutil.Process(start_task_resp.PID)
    process_stats["memory_kb"] = (
        psutil_process.memory_full_info().uss / 1024
    )  # convert to KB

    return process_stats
