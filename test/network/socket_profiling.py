import csv
import re
import subprocess
import time
import argparse
import psutil
from scapy.all import TCP, sniff
from threading import Thread
from concurrent.futures import ThreadPoolExecutor

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

def process_tcpdump_output(port_filters):
    cmd = ['sudo', 'tcpdump', '-l', '-S', '-i', 'any', port_filters]  # Replace with your tcpdump command
    print(cmd)
    with subprocess.Popen(cmd, stdout=subprocess.PIPE, universal_newlines=True) as proc, open('tcpdump_output.csv', 'w', newline='') as csvfile:
        fieldnames = ['timestamp', 'iface', 'src', 'dest', 'flags', 'seq_start', 'seq_end', 'ack', 'win', 'options', 'length']
        csv_writer = csv.DictWriter(csvfile, fieldnames=fieldnames)
        csv_writer.writeheader()

        for line in proc.stdout:
            print(line)
            pattern = re.compile(r'(\d+:\d+:\d+\.\d+) (\w+)\s+In\s+IP ([^ ]+) > ([^:]+): Flags \[([^\]]+)\], seq (\d+):(\d+), ack (\d+), win (\d+), options \[([^\]]+)\], length (\d+)')
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
    parser = argparse.ArgumentParser(description='TCP resilience testing.')
    parser.add_argument('--delay', type=int, default=100, help='Network latency in ms')
    parser.add_argument('--jitter', type=int, default=10, help='Jitter in ms')
    parser.add_argument('--loss', type=float, default=0.3, help='Packet loss rate in percentage')
    
    args = parser.parse_args()
 #   setup_env(args.delay, args.jitter, args.loss)

    command = "sudo ./../../cedana start -p 'python3 server_client.py --mode server'"
    process = subprocess.Popen(["sh", "-c", command], stdout=subprocess.PIPE)
    pid = int(process.communicate()[0].decode().strip())
    print("Started process with PID {}".format(pid))

    # start monitoring tcp seq data 
    # wait a few seconds before starting 
    time.sleep(5)
    ports = get_ports(pid)
    print(ports)
    if ports:
        port_filters = " or ".join([f"port {port}" for port in ports])
        print(port_filters)
        with ThreadPoolExecutor() as executor:
            executor.submit(process_tcpdump_output, port_filters)
    else:
        print("No ports found.")
