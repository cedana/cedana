import os
import subprocess

def main(): 
 #   start_cedana_daemon()
    # daemon is running, tell it to start process 
    
    # clone repo url first 
    repo_url = "https://github.com/cedana/cedana-benchmarks"
    subprocess.run(["git", "clone", repo_url])

    client_process = "python3 cedana-benchmarks/networking/threaded_pings.py -n 3 google.com 80" 
    exec_cedana_process(client_process)

def start_cedana_daemon():
    subprocess.Popen(["sudo", "./../../cedana", "daemon"], shell=False)
def exec_cedana_process(process): 
    subprocess.Popen(["sudo", "./../../cedana", "start", process], shell=False)


main()