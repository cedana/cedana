#!/usr/bin/env python3
"""
A producer and a consumer passing messages through shared memory, as processes of an
inference server do: the producer writes each message's payload into a POSIX shared memory
slot, then sends its sequence number through a multiprocessing.Queue (a pipe and POSIX
semaphores in /dev/shm). The consumer requires every sequence number, in order, with the
payload that goes with it, and logs its progress. Many messages are in flight at any time,
so a checkpoint catches some in the queue, the pipe, and the slots.

Usage: shm-messages.py <log>
"""

import multiprocessing as mp
import sys
import time
from multiprocessing import shared_memory

SLOTS, SLOT = 64, 4096


def payload(seq):
    return (seq.to_bytes(8, "little") * (SLOT // 8))


def producer(name, queue, free):
    shm = shared_memory.SharedMemory(name=name)
    seq = 0
    while True:
        free.acquire()  # a slot the consumer is done with
        slot = seq % SLOTS
        shm.buf[slot * SLOT:(slot + 1) * SLOT] = payload(seq)
        queue.put(seq)
        seq += 1


def consumer(name, queue, free, log_path):
    shm = shared_memory.SharedMemory(name=name)
    log = open(log_path, "a", buffering=1)
    expected = 0
    last_log = 0.0
    while True:
        seq = queue.get()
        slot = seq % SLOTS
        if seq != expected or bytes(shm.buf[slot * SLOT:(slot + 1) * SLOT]) != payload(seq):
            log.write(f"MISMATCH got {seq}, expected {expected}\n")
            sys.exit(1)
        free.release()
        expected += 1
        if time.monotonic() - last_log > 0.2:
            log.write(f"SEQ {seq}\n")
            last_log = time.monotonic()


def main():
    log_path = sys.argv[1]
    mp.set_start_method("fork")
    shm = shared_memory.SharedMemory(create=True, size=SLOTS * SLOT)
    queue = mp.Queue()
    free = mp.Semaphore(SLOTS)
    procs = [
        mp.Process(target=producer, args=(shm.name, queue, free)),
        mp.Process(target=consumer, args=(shm.name, queue, free, log_path)),
    ]
    for p in procs:
        p.start()
    with open(log_path, "a") as log:
        log.write("READY\n")
    for p in procs:
        p.join()
        if p.exitcode:
            sys.exit(p.exitcode)


if __name__ == "__main__":
    main()
