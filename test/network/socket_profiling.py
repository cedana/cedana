import os
import subprocess

def main(): 
 #   start_cedana_daemon()
    # daemon is running, tell it to start process 
    client_process = "python3 multiconn_client.py -n 3 google.com 80" 
    exec_cedana_process(client_process)

def start_cedana_daemon():
    subprocess.Popen(["sudo", "./../../cedana", "daemon"], shell=False)

def exec_cedana_process(process): 
    result = subprocess.Popen(["sudo", "./../../cedana", "start -p", process], shell=False)
    print(result.stderr)


main()