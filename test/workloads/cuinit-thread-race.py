#!/usr/bin/env python3
"""
Two threads whose first CUDA call is a simultaneous cuInit, each then allocating device memory.

Usage: cuinit-thread-race.py
"""

import ctypes
import threading

cuda = ctypes.CDLL('libcuda.so.1')
barrier = threading.Barrier(2)
failures = []


def check(name, *args):
    result = getattr(cuda, name)(*args)
    if result:
        raise RuntimeError(f'{name} returned {result}')


def work():
    barrier.wait()
    try:
        device, context, pointer = ctypes.c_int(), ctypes.c_void_p(), ctypes.c_uint64()
        check('cuInit', 0)
        check('cuDeviceGet', ctypes.byref(device), 0)
        check('cuDevicePrimaryCtxRetain', ctypes.byref(context), device)
        check('cuCtxSetCurrent', context)
        check('cuMemAlloc_v2', ctypes.byref(pointer), ctypes.c_size_t(4))
        check('cuMemFree_v2', pointer)
    except Exception as failure:
        failures.append(failure)


threads = [threading.Thread(target=work) for _ in range(2)]
for thread in threads:
    thread.start()
for thread in threads:
    thread.join()
if failures:
    raise failures[0]
