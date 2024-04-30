import csv
import platform
import psutil
import os
import signal
import subprocess
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


def run_checkpoint(daemonPID, jobID, output_dir, process_stats):
    chkpt_cmd = "sudo -E ./cedana dump job {} -d tmp".format(jobID)
    # initial data here is fine - we want to measure impact of daemon on system
    initial_data = start_recording(daemonPID)
    cpu_profile_filename = "{}/cpu_{}_checkpoint".format(output_dir, jobID)

    p = subprocess.Popen(["sh", "-c", chkpt_cmd], stdout=subprocess.PIPE)
    # used for capturing full time instead of directly exiting
    p.wait()

    time.sleep(5)
    stop_recording("checkpoint", daemonPID, initial_data, jobID, process_stats)


def run_restore(daemonPID, jobID, output_dir):
    restore_cmd = "sudo -E ./cedana restore job {}".format(jobID)
    initial_data = start_recording(daemonPID)
    cpu_profile_filename = "{}/cpu_{}_restore".format(output_dir, jobID)

    p = subprocess.Popen(["sh", "-c", restore_cmd], stdout=subprocess.PIPE)
    p.wait()

    # nil value here
    process_stats = {}
    process_stats["memory_kb"] = 0

    time.sleep(5)
    stop_recording("restore", daemonPID, initial_data, jobID, process_stats)


def run_exec(cmd, jobID):
    process_stats = {}
    exec_cmd = "cedana exec -w $PWD/benchmarks {} {}".format(cmd, jobID)

    process = subprocess.Popen(["sh", "-c", exec_cmd], stdout=subprocess.PIPE)
    pid = int(process.communicate()[0].decode().strip())
    process_stats["pid"] = pid

    psutil_process = psutil.Process(pid)
    process_stats["memory_kb"] = (
        psutil_process.memory_full_info().uss / 1024
    )  # convert to KB

    return process_stats 
