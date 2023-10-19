import csv
import re
import subprocess
import time
import psutil
import concurrent.futures

def setup_network(delay, jitter, loss):
    subprocess.run(["sudo", "tc", "qdisc", "add", "dev", "eth0", "root", "netem", f"delay {delay}ms", f"jitter {jitter}ms", f"loss {loss}%"])

def cleanup_network():
    subprocess.run(["sudo", "tc", "qdisc", "del", "dev", "eth0", "root"])

def setup_env(delay, jitter, loss):
    print("Setting up environment...")
    setup_network(delay, jitter, loss)

def teardown_env():
    print("Tearing down environment...")
    cleanup_network()

def get_ports(pid):
    try:
        proc = psutil.Process(pid)
    except psutil.NoSuchProcess:
        print(f"Process with PID {pid} not found.")
        return []

    ports = [conn.laddr.port for conn in proc.connections(kind='all')]
    return ports


def start_tcpdump_capture(port_filters):
    cmd = ['sudo', 'tcpdump', '-l', '-S', '-i', 'any', port_filters]
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, bufsize=1, universal_newlines=True)
    return proc

# reading tcpdump directly instead of using scapy because it works w/ localhost as well

def process_tcpdump_output(proc):
    proc.terminate()
    stdout, _ = proc.communicate()

    with open('tcpdump_output.csv', 'w', newline='') as csvfile:
        fieldnames = ['timestamp', 'iface', 'src', 'dest', 'flags', 'seq_start', 'seq_end', 'ack', 'win', 'options', 'length']
        csv_writer = csv.DictWriter(csvfile, fieldnames=fieldnames)
        csv_writer.writeheader()

        pattern = re.compile(r'(\d+:\d+:\d+\.\d+) (\w+)\s+In\s+IP ([^ ]+) > ([^:]+): Flags \[([^\]]+)\], seq (\d+):(\d+), ack (\d+), win (\d+), options \[([^\]]+)\], length (\d+)')

        for line in stdout.split('\n'):
            match = pattern.match(line.strip())
            if match:
                timestamp, iface, src, dest, flags, seq_start, seq_end, ack, win, options, length = match.groups()
                csv_writer.writerow({
                    'timestamp': timestamp,
                    'iface': iface,
                    'src': src,
                    'dest': dest,
                    'flags': flags,
                    'seq_start': seq_start,
                    'seq_end': seq_end,
                    'ack': ack,
                    'win': win,
                    'options': options,
                    'length': length,
                })

if __name__ == "__main__":
    command = "sudo ./cedana exec 'python3 test/network/server_client.py --mode server' socket_test"
    process = subprocess.Popen(["sh", "-c", command], stdout=subprocess.PIPE)
    pid = int(process.communicate()[0].decode().strip())
    print("Started process with PID {}".format(pid))

    # start monitoring tcp seq data 
    # wait a few seconds before starting 
    time.sleep(5)
    ports = [8001]
    print(ports)
    if ports:
        port_filters = " or ".join([f"port {port}" for port in ports])
        print(port_filters)
        proc = start_tcpdump_capture(port_filters)  # Start capturing
    else:
        print("No ports found.")

    # checkpoint 
    time.sleep(20)
    checkpoint_started_at = time.time()
    print("starting dump of process...")
    chkpt_cmd = "sudo ./cedana dump job {} -d tmp".format("socket_test")
    process = subprocess.Popen(["sh", "-c", chkpt_cmd], stdout=subprocess.PIPE)
    checkpoint_completed_at = time.time()

    # instant restore
    # restore (from outside for now)
    time.sleep(10)
    restore_started_at = time.time()
    print("starting restore of process...")
    restore_cmd = "sudo ./cedana restore job {}".format("socket_test")
    process = subprocess.Popen(["sh", "-c", restore_cmd], stdout=subprocess.PIPE)
    restore_completed_at = time.time()

    process_tcpdump_output(proc)

