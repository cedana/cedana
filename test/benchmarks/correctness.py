import cedana_bindings as cedana
from process_json import diff_ckpts, print_results
import psutil
import time
from tplib import task_pb2

def bold(txt: str):
    return "\033[1m" + txt + "\033[0m"

def print_test(test: str, job: str, cmd: str):
    test, job, cmd = bold(test), bold(job), bold(cmd)
    print("\nStarting correctness test {} for job {} with command {}".format(test, job, cmd))

def print_stats(stats1, stats2):
    print("job 1 stats:\n", stats1)
    print("job 2 stats:\n", stats2)

def print_done(jobID1, jobID2):
    def purple(txt: str) -> str:
        return "\u001b[35m" + txt + "\033[0m"
    jobID1, jobID2 = purple(jobID1), purple(jobID2)
    print("[PROCESS {} AND PROCESS {} DONE.]".format(jobID1, jobID2))

def print_res(names: list[str], results: list[bool]):
    assert len(results) > 0
    assert len(names) == len(results)
    print()
    overall = True
    for name, result in zip(names, results):
        overall &= result
        print("\033[1;32mPASS\033[0m" if result else "\033[1;31mFAIL\033[0m", name, "test")
    print("-"*20)
    print("\033[1;32mPASS\033[0m" if overall else "\033[1;31mFAIL\033[0m", bold("CORRECTNESS"))

async def base(job: str, cmd: str, verbose: bool) -> bool:
    print_test("base", job, cmd) if verbose else None
    jobID1 = job + "-base-1"
    jobID2 = job + "-base-2"
    stats1 = await cedana.run_exec(cmd+" "+jobID1, jobID1)
    stats2 = await cedana.run_exec(cmd+" "+jobID2, jobID2)
    print_stats(stats1, stats2) if verbose else None
    while stats1["pid"] in psutil.pids() or stats2["pid"] in psutil.pids():
        continue
    print_done(jobID1, jobID2) if verbose else None
    return diff_ckpts(jobID1, jobID2, "terminal_base.csv", verbose)

async def c2r2(job: str, cmd: str, daemon_pid: int, remote: bool, verbose: bool) -> bool:
    print_test("ckpt2 restore2", job, cmd) if verbose else None
    jobID1 = job + "-c2r2-1"
    jobID2 = job + "-c2r2-2"
    stats1 = await cedana.run_exec(cmd+" "+jobID1, jobID1)
    stats2 = await cedana.run_exec(cmd+" "+jobID2, jobID2)
    resp2 = await cedana.run_checkpoint(daemon_pid, jobID2, cedana.output_dir, stats2, remote)
    await cedana.run_restore(daemon_pid, jobID2, resp2.CheckpointID, cedana.output_dir, remote)
    print_stats(stats1, stats2) if verbose else None
    while stats1["pid"] in psutil.pids() or stats2["pid"] in psutil.pids():
        continue
    print_done(jobID1, jobID2) if verbose else None
    return diff_ckpts(jobID1, jobID2, "terminal_c1r1.csv", verbose)

async def c2r2c2r2(job: str, cmd: str, daemon_pid: int, remote: bool, verbose: bool) -> bool:
    print_test("ckpt2 restore2 ckpt2 restore2", job, cmd) if verbose else None
    jobID1 = job + "-c2r2c2r2-1"
    jobID2 = job + "-c2r2c2r2-2"
    stats1 = await cedana.run_exec(cmd+" "+jobID1, jobID1)
    stats2 = await cedana.run_exec(cmd+" "+jobID2, jobID2)
    resp2 = await cedana.run_checkpoint(daemon_pid, jobID2, cedana.output_dir, stats2, remote)
    await cedana.run_restore(daemon_pid, jobID2, resp2.CheckpointID, cedana.output_dir, remote)
    resp2 = await cedana.run_checkpoint(daemon_pid, jobID2, cedana.output_dir, stats2, remote)
    await cedana.run_restore(daemon_pid, jobID2, resp2.CheckpointID, cedana.output_dir, remote)
    print_stats(stats1, stats2) if verbose else None
    while stats1["pid"] in psutil.pids() or stats2["pid"] in psutil.pids():
        continue
    print_done(jobID1, jobID2) if verbose else None
    return diff_ckpts(jobID1, jobID2, "terminal_c1r1c1r1.csv", verbose)

async def c1c2r1r2(job: str, cmd: str, daemon_pid: int, remote: bool, verbose: bool) -> bool:
    print_test("ckpt1 ckpt2 restore1 restore2", job, cmd) if verbose else None
    jobID1 = job + "-c1c2r1r2-1"
    jobID2 = job + "-c1c2r1r2-2"
    stats1 = await cedana.run_exec(cmd+" "+jobID1, jobID1)
    stats2 = await cedana.run_exec(cmd+" "+jobID2, jobID2)
    resp1 = await cedana.run_checkpoint(daemon_pid, jobID1, cedana.output_dir, stats1, remote)
    resp2 = await cedana.run_checkpoint(daemon_pid, jobID2, cedana.output_dir, stats2, remote)
    await cedana.run_restore(daemon_pid, jobID1, resp1.CheckpointID, cedana.output_dir, remote)
    await cedana.run_restore(daemon_pid, jobID2, resp2.CheckpointID, cedana.output_dir, remote)
    time.sleep(1)
    print_stats(stats1, stats2) if verbose else None
    while stats1["pid"] in psutil.pids() or stats2["pid"] in psutil.pids():
        continue
    print_done(jobID1, jobID2) if verbose else None
    return diff_ckpts(jobID1, jobID2, "terminal_c1c2r1r2.csv", verbose)

async def c1r1c2r2(job: str, cmd: str, daemon_pid: int, remote: bool, verbose: bool) -> bool:
    print_test("ckpt1 restore1 ckpt2 restore2", job, cmd) if verbose else None
    jobID1 = job + "-c1r1c2r2-1"
    jobID2 = job + "-c1r1c2r2-2"
    stats1 = await cedana.run_exec(cmd+" "+jobID1, jobID1)
    stats2 = await cedana.run_exec(cmd+" "+jobID2, jobID2)
    resp1 = await cedana.run_checkpoint(daemon_pid, jobID1, cedana.output_dir, stats1, remote)
    await cedana.run_restore(daemon_pid, jobID1, resp1.CheckpointID, cedana.output_dir, remote)
    time.sleep(1)
    resp2 = await cedana.run_checkpoint(daemon_pid, jobID2, cedana.output_dir, stats2, remote)
    await cedana.run_restore(daemon_pid, jobID2, resp2.CheckpointID, cedana.output_dir, remote)
    time.sleep(1)
    print_stats(stats1, stats2) if verbose else None
    while stats1["pid"] in psutil.pids() or stats2["pid"] in psutil.pids():
        continue
    print_done(jobID1, jobID2) if verbose else None
    return diff_ckpts(jobID1, jobID2, "terminal_c1r1c2r2.csv", verbose)

async def main(daemon_pid: int, remote: bool, verbose: bool):
    job = "nn-1gb"
    cmd = "python3 1gb_pytorch_correctness.py"
    base_ = await base(job, cmd, verbose)
    c2r2_ = await c2r2(job, cmd, daemon_pid, remote, verbose)
    c1c2r1r2_ = await c1c2r1r2(job, cmd, daemon_pid, remote, verbose)
    c1r1c2r2_ = await c1r1c2r2(job, cmd, daemon_pid, remote, verbose)
    c2r2c2r2_ = await c2r2c2r2(job, cmd, daemon_pid, remote, verbose)
    names = ["base", "c2r2", "c1c2r1r2", "c1r1c2r2", "c2r2c2r2"]
    results = [base_, c2r2_, c1c2r1r2_, c1r1c2r2_, c2r2c2r2_]
    print_res(names, results)
    return "correctness-data-" + str(time.time())
