#!/usr/bin/env python3
"""
Models tensor-parallel ranks that map the same JIT-compiled library, which a late rank then
replaces under the same name (os.replace): every process maps one unlinked inode (a ghost
file for CRIU), whose name is busy with another file. Each process checks its mapping, and
the parent the file at the name, every 0.2s, logging to the log file.

Usage: ghost-busy-name.py <dir> <log>
"""

import mmap
import os
import sys
import time

CHILDREN = 4
PAGE = mmap.PAGESIZE
GHOST, LIVE = b"g", b"l"


def log(f, msg):
    f.write(f"{msg}\n")
    f.flush()


def check_forever(f, name, mem, path=None):
    count = 0
    while True:
        if mem[:] != GHOST * (2 * PAGE):
            log(f, f"MISMATCH {name}: mapping changed")
            sys.exit(1)
        if path is not None:
            with open(path, "rb") as live:
                if live.read() != LIVE * (2 * PAGE):
                    log(f, f"MISMATCH {name}: file at {path} changed")
                    sys.exit(1)
        log(f, f"OK {name} {count}")
        count += 1
        time.sleep(0.2)


def main():
    directory, log_path = sys.argv[1], sys.argv[2]
    lib = os.path.join(directory, "cuda_utils.so")
    with open(lib, "wb") as out:
        out.write(GHOST * (2 * PAGE))

    f = open(log_path, "a", buffering=1)
    mapped_r, mapped_w = os.pipe()

    for i in range(CHILDREN):
        if os.fork() == 0:
            os.close(mapped_r)
            with open(lib, "rb") as lf:
                mem = mmap.mmap(lf.fileno(), 2 * PAGE, prot=mmap.PROT_READ)
            os.write(mapped_w, b"x")
            check_forever(f, f"child{i}", mem)

    os.close(mapped_w)
    for _ in range(CHILDREN):
        os.read(mapped_r, 1)
    with open(lib, "rb") as lf:
        mem = mmap.mmap(lf.fileno(), 2 * PAGE, prot=mmap.PROT_READ)

    # The late rank's replacement: the mapped inode is unlinked, its name now busy
    tmp = lib + ".tmp"
    with open(tmp, "wb") as out:
        out.write(LIVE * (2 * PAGE))
    os.replace(tmp, lib)

    log(f, "READY")
    check_forever(f, "parent", mem, lib)


if __name__ == "__main__":
    main()
