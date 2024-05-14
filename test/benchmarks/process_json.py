import filecmp
import glob
import json
import os
import subprocess
import sys

# return most recent terminal checkpoint for this jobID
def find_dir(jobID: str) -> str:
    directory = "/terminal-ckpt/"
    entries = [entry for entry in os.listdir(directory) if entry.startswith(jobID) and not entry.endswith(".tar")]
    if not entries:
        return None
    full_paths = [os.path.join(directory, entry) for entry in entries]
    return max(full_paths, key=os.path.getmtime)

def print_result(name: str, result: bool, err: str=""):
    if result:
        print("\033[1;32mPASS\033[0m", name)
    else:
        print("\033[1;31mFAIL\033[0m", name + ": " + err if err else name)

# compare dir1/* and dir2/* that should be identical
def diff_other(dir1: str, dir2: str, verbose: bool) -> (bool, str):
    files = ["inventory.json", "fdinfo-2.json", "seccomp.json"]
    if verbose:
        for file in files:
            file1 = os.path.join(dir1, file)
            file2 = os.path.join(dir2, file)
            assert os.path.exists(file1)
            assert os.path.exists(file2)
            res = filecmp.cmp(file1, file2)
            if not res:
                return res, file1 + " and " + file2 + " differ"
        fs1 = glob.glob(os.path.join(dir1, "fs-*.json"))
        fs2 = glob.glob(os.path.join(dir2, "fs-*.json"))
        assert len(fs1) == 1
        assert len(fs2) == 1
        res = filecmp.cmp(*fs1, *fs2)
        return res, str(*fs1) + " and" + str(*fs2) + " differ"
    else:
        res = True
        for file in files:
            file1 = os.path.join(dir1, file)
            file2 = os.path.join(dir2, file)
            assert os.path.exists(file1)
            assert os.path.exists(file2)
            res &= filecmp.cmp(file1, file2)
        fs1 = glob.glob(os.path.join(dir1, "fs-*.json"))
        fs2 = glob.glob(os.path.join(dir2, "fs-*.json"))
        assert len(fs1) == 1
        assert len(fs2) == 1
        res &= filecmp.cmp(*fs1, *fs2)
        return res, "other files differ"

def diff_files(dir1: str, dir2: str) -> (bool, str):
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
            return False, "differing entries"
        if entry2 not in entries1 and not entry2["reg"]["name"].startswith("/var/log/cedana-output"):
            return False, "differing entries"
    return True, ""

def diff_mm(dir1: str, dir2: str) -> (bool, str):
    def prep_mm(dir_path: str) -> dict:
        mm_json_path = glob.glob(os.path.join(dir_path, "mm-*.json")) # need to autocomplete with PID
        assert len(mm_json_path) == 1 # todo: sometimes multiple
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
        return False, "differing fields"
    for vma1, vma2 in zip(vmas1, vmas2):
        if vma1 not in vmas2 or vma2 not in vmas1:
            return False, "differing vmas"
    return True, ""

def diff_pagemap(dir1: str, dir2: str) -> (bool, str):
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

    if index1 != index2:
        return False, "differing indices"

    misses = 0
    for entry1, entry2 in zip(entries1, entries2):
        if entry1 not in entries2 or entry2 not in entries1:
            misses += 1
    err = "misses / len(entries) = " + str(misses/len(entries1)) + " should be < 0.25"
    return (misses / len(entries1) < 0.25), err # todo: verify threshold

def diff_ckpts(jobID1: str, jobID2: str, verbose: bool) -> bool:
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
    test_files, err_files = diff_files(dir1, dir2)
    test_mm, err_mm = diff_mm(dir1, dir2)
    test_pagemap, err_pagemap = diff_pagemap(dir1, dir2)
    test_other, err_other = diff_other(dir1, dir2, verbose)
    if verbose:
        print_result("files test", test_files, err_files)
        print_result("mm-<PID> test", test_mm, err_mm)
        print_result("pagemap-<PID> test", test_pagemap, err_pagemap)
        print_result("other test", test_other, err_other)
        print("-"*20)
    return (test_files and test_mm and test_pagemap and test_other)

if __name__ == "__main__":
    verbose = ("--verbose" in sys.argv)
    result = diff_ckpts("nn-1gb-base", "nn-1gb-saved", verbose)
    print_result("overall", result) if verbose else None
