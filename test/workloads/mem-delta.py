#!/usr/bin/env python3
"""Holds a large, mostly static buffer and rewrites one page per tick.

Usage: mem-delta.py [size_mib] [ticks] [tick_secs]

Built for incremental dumps: between two dumps only a few pages change, so a delta
on top of the previous dump should be tiny. On exit it rebuilds the expected buffer
from scratch and compares, so a restore that lost pages (e.g. a broken parent chain)
prints MISMATCH and exits 1.
"""

import hashlib
import random
import sys
import time

PAGE = 4096
HOT_SPAN = 16 * 1024 * 1024  # writes stay in the first 16 MiB

size = int(sys.argv[1]) * 1024 * 1024 if len(sys.argv) > 1 else 256 * 1024 * 1024
ticks = int(sys.argv[2]) if len(sys.argv) > 2 else 150
tick_secs = float(sys.argv[3]) if len(sys.argv) > 3 else 0.2


def fresh():
    # Random (not zero) so nothing is elided as zero pages; randbytes caps out below 256 MiB
    rng = random.Random(1234)
    buf = bytearray()
    for _ in range(0, size, 1 << 20):
        buf += rng.randbytes(min(1 << 20, size - len(buf)))
    return buf


def write(buf, k):
    off = (k * 7919 * PAGE) % min(HOT_SPAN, size)
    buf[off : off + 8] = k.to_bytes(8, "little")


buf = fresh()
print(f"started size={size} ticks={ticks}", flush=True)
for k in range(ticks):
    write(buf, k)
    if k % 10 == 0:
        print(f"tick {k}", flush=True)
    time.sleep(tick_secs)

expected = fresh()
for k in range(ticks):
    write(expected, k)

got, want = hashlib.sha256(buf).hexdigest(), hashlib.sha256(expected).hexdigest()
if got != want:
    print(f"MISMATCH got={got} want={want}", flush=True)
    sys.exit(1)
print(f"OK {got}", flush=True)
