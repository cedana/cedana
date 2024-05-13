import filecmp
import glob
import json
import os
import subprocess
import sys

verbose = False

# return most recent terminal checkpoint for this jobID
def find_dir(jobID: str) -> str:
    directory = "/terminal-ckpt/"
    entries = [entry for entry in os.listdir(directory) if entry.startswith(jobID) and not entry.endswith(".tar")]
    if not entries:
        return None
    full_paths = [os.path.join(directory, entry) for entry in entries]
    return max(full_paths, key=os.path.getmtime)

def print_result(test_names: list[str], results: list[bool]):
    if not verbose:
        return
    assert len(test_names) == len(results)
    for test_name, result in zip(test_names, results):
        if result:
            print("\033[1;32mPASS\033[0m", test_name, "test")
        else:
            print("\033[1;31mFAIL\033[0m", test_name, "test")

# compare dir1/* and dir2/* that should be identical
def diff_other(dir1: str, dir2: str) -> bool:
    files = ["inventory.json", "fdinfo-2.json", "seccomp.json"]
    res = True
    for file in files:
        res &= filecmp.cmp(os.path.join(dir1, file), os.path.join(dir2, file))
    fs1 = glob.glob(os.path.join(dir1, "fs-*.json"))
    fs2 = glob.glob(os.path.join(dir2, "fs-*.json"))
    assert len(fs1) == 1
    assert len(fs2) == 1
    res &= filecmp.cmp(*fs1, *fs2)
    return res

def diff_files(dir1: str, dir2: str) -> bool:
    def prep_files(dir_path: str) -> dict:
        files_json_path = os.path.join(dir_path, "files.json")
        assert os.path.exists(files_json_path)
        f = open(files_json_path)
        entries = json.load(f)["entries"]
        f.close()
        entries = [entry for entry in entries if entry.get("TYPE") == "REG"]
        for entry in entries:
            entry.pop("id")
            entry["reg"].pop("id")
        return entries

    entries1 = prep_files(dir1)
    entries2 = prep_files(dir2)

    for entry1, entry2 in zip(entries1, entries2):
        if entry1 not in entries2 and not entry1["reg"]["name"].startswith("/var/log/cedana-output"):
            return False
        if entry2 not in entries1 and not entry2["reg"]["name"].startswith("/var/log/cedana-output"):
            return False
    return True

def diff_mm(dir1: str, dir2: str) -> bool:
    def prep_mm(dir_path: str) -> dict:
        mm_json_path = glob.glob(os.path.join(dir_path, "mm-*.json")) # need to autocomplete with PID
        assert len(mm_json_path) == 1
        f = open(*mm_json_path)
        fields = json.load(f)["entries"][0]
        f.close()
        vmas = fields.pop("vmas")
        fields.pop("mm_start_code")
        fields.pop("mm_end_code")
        fields.pop("mm_start_data")
        fields.pop("mm_end_data")
        fields.pop("mm_start_stack")
        fields.pop("mm_start_brk")
        fields.pop("mm_brk")
        fields.pop("mm_arg_start")
        fields.pop("mm_arg_end")
        fields.pop("mm_env_start")
        fields.pop("mm_env_end")
        fields.pop("mm_saved_auxv")
        for vma in vmas:
            vma.pop("start")
            vma.pop("end")
            vma.pop("shmid")
        return fields, vmas

    fields1, vmas1 = prep_mm(dir1)
    fields2, vmas2 = prep_mm(dir2)

    if fields1 != fields2:
        return False
    for vma1, vma2 in zip(vmas1, vmas2):
        if vma1 not in vmas2 or vma2 not in vmas1:
            return False
    return True

def diff_pagemap(dir1: str, dir2: str) -> bool:
    def prep_pagemap(dir_path: str) -> dict:
        pagemap_json_path = glob.glob(os.path.join(dir_path, "pagemap-*.json")) # need to autocomplete with PID
        assert len(pagemap_json_path) == 1
        f = open(*pagemap_json_path)
        entries = json.load(f)["entries"]
        index = entries[0]
        entries = entries[1:]
        f.close()
        for entry in entries:
            entry.pop("vaddr")
        return index, entries

    index1, entries1 = prep_pagemap(dir1)
    index2, entries2 = prep_pagemap(dir2)

    if index1 != index2 or len(entries1) != len(entries2):
        return False

    misses = 0
    for entry1, entry2 in zip(entries1, entries2):
        if entry1 not in entries2 or entry2 not in entries1:
            misses += 1
    return (misses / len(entries1) < 0.25) # todo: verify threshold

def diff_ckpts(jobID1: str, jobID2: str) -> bool:
    if verbose:
        print("DIFFING JOBS \""+jobID1+"\" AND \""+jobID2+"\"...")
    dir1 = find_dir(jobID1)
    dir2 = find_dir(jobID2)
    if verbose:
        print("FOUND", dir1+"/...")
        print("FOUND", dir2+"/...")
        print("-"*20)
        print("TESTING...")
    subprocess.run(["cedana", "perf", "crit", "show", dir1], capture_output=True, text=True)
    subprocess.run(["cedana", "perf", "crit", "show", dir2], capture_output=True, text=True)
    test_files = diff_files(dir1, dir2)
    test_mm = diff_mm(dir1, dir2)
    test_pagemap = diff_pagemap(dir1, dir2)
    test_other = diff_other(dir1, dir2)
    print_result(["files", "mm-<PID>", "pagemap-<PID>", "other"], [test_files, test_mm, test_pagemap, test_other])
    if verbose:
        print("-"*20)
    return (test_files and test_mm and test_pagemap and test_other)

if __name__ == "__main__":
    verbose = ("--verbose" in sys.argv)
    result = diff_ckpts("nn-1gb-base","nn-1gb-saved")
    print_result(["all"], [result])
