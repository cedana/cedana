#!/usr/bin/env python3
"""
Sets an attribute on a kernel of a library and unloads the library, then loads a second library,
which under Cedana gets the freed handle from the GPU worker's driver. Increments a device counter
until the gate file exists, printing READY after the first launch, then prints whether the handle
was reused.

Usage: library-handle-reuse.py <gate>
"""

import ctypes
import os
import sys
import time

INCREMENT = b"""
.version 7.0
.target sm_70
.address_size 64
.visible .entry increment(.param .u64 counter)
{
  .reg .b32 %r<2>;
  .reg .b64 %rd<3>;
  ld.param.u64 %rd1, [counter];
  cvta.to.global.u64 %rd2, %rd1;
  ld.global.u32 %r1, [%rd2];
  add.s32 %r1, %r1, 1;
  st.global.u32 [%rd2], %r1;
  ret;
}
"""
STEP = b"""
.version 7.0
.target sm_70
.address_size 64
.visible .entry step()
{
  ret;
}
"""
STRIDE = b"""
.version 7.0
.target sm_70
.address_size 64
.visible .entry stride()
{
  ret;
}
"""
CU_FUNC_ATTRIBUTE_MAX_DYNAMIC_SHARED_SIZE_BYTES = 8

cuda = ctypes.CDLL('libcuda.so.1')


def check(name, *args):
    result = getattr(cuda, name)(*args)
    if result:
        raise RuntimeError(f'{name} returned {result}')


gate = sys.argv[1]
device, context = ctypes.c_int(), ctypes.c_void_p()
check('cuInit', 0)
check('cuDeviceGet', ctypes.byref(device), 0)
check('cuDevicePrimaryCtxRetain', ctypes.byref(context), device)
check('cuCtxSetCurrent', context)

# Loaded before the unload, so the second library is the one that gets the freed handle.
module, kernel, counter = ctypes.c_void_p(), ctypes.c_void_p(), ctypes.c_uint64()
check('cuModuleLoadData', ctypes.byref(module), INCREMENT)
check('cuModuleGetFunction', ctypes.byref(kernel), module, b'increment')
check('cuMemAlloc_v2', ctypes.byref(counter), ctypes.c_size_t(4))
check('cuMemsetD32_v2', counter, 0, ctypes.c_size_t(1))

first, step_module, step = ctypes.c_void_p(), ctypes.c_void_p(), ctypes.c_void_p()
check('cuLibraryLoadData', ctypes.byref(first), STEP, None, None, 0, None, None, 0)
check('cuLibraryGetModule', ctypes.byref(step_module), first)
check('cuModuleGetFunction', ctypes.byref(step), step_module, b'step')
check('cuFuncSetAttribute', step, CU_FUNC_ATTRIBUTE_MAX_DYNAMIC_SHARED_SIZE_BYTES, 16 * 1024)
check('cuLibraryUnload', first)

second, stride = ctypes.c_void_p(), ctypes.c_void_p()
check('cuLibraryLoadData', ctypes.byref(second), STRIDE, None, None, 0, None, None, 0)
check('cuLibraryGetKernel', ctypes.byref(stride), second, b'stride')
reused = second.value == first.value

params = (ctypes.c_void_p * 1)(ctypes.addressof(counter))
launches = 0
while True:
    check('cuLaunchKernel', kernel, 1, 1, 1, 1, 1, 1, 0, None, params, None)
    check('cuCtxSynchronize')
    launches += 1
    if launches == 1:
        print('READY', flush=True)
    if os.path.exists(gate):
        break
    time.sleep(0.1)

value = ctypes.c_uint32()
check('cuMemcpyDtoH_v2', ctypes.byref(value), counter, ctypes.c_size_t(4))
if value.value != launches:
    raise RuntimeError(f'counter {value.value} after {launches} launches')
check('cuLibraryGetKernel', ctypes.byref(stride), second, b'stride')
print('library handle', 'reused' if reused else 'not reused')
