#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# CUDA API patterns real applications use, which crash or fail under the interceptor, or
# after a restore, when it doesn't handle them. Each sample checks its own results, and
# prints PASS and exits 0 only if they're right.
#
# bats file_tags=gpu,gpuapi

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget

SAMPLES=/cedana-samples/gpu_smr

setup_file() {
    trap_gpu_signals
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    setup_file_daemon
}

setup() {
    trap_gpu_signals
    setup_daemon
}

teardown() {
    # A sample that hung: timeout only stopped the attached CLI, and a persistent daemon
    # would keep it (and its GPU controller) running into the next tests
    if [ -n "${SAMPLE_JID:-}" ]; then
        cedana job kill "$SAMPLE_JID" &>/dev/null || true
    fi
    teardown_daemon
    check_gpu_signals
}

teardown_file() {
    teardown_file_daemon
    check_gpu_signals
}

require_sample() {
    if [ ! -x "$SAMPLES/$1" ]; then
        skip "sample $1 is not in this image (needs cedana-samples v0.4.3+)"
    fi
}

# Runs a self-verifying sample under the interceptor to its end: it must print PASS
run_sample() {
    local sample="$1"
    require_sample "$sample"

    SAMPLE_JID=$(unix_nano) # for teardown
    run timeout 180 cedana run process --attach -g --jid "$SAMPLE_JID" -- "$SAMPLES/$sample"
    assert_success
    assert_output --partial PASS
}

# Checkpoints a gated sample mid-run (once it's READY), restores it, then lets it finish: it
# must print PASS, all its state having come back exact
restore_sample() {
    local sample="$1"
    require_sample "$sample"

    local jid gate log
    jid=$(unix_nano)
    SAMPLE_JID=$jid # for teardown
    gate="/tmp/cedana-gate-$jid"

    # Bounded, as the attached restore below is: a hang fails this test, not the whole job
    timeout 120 cedana run process -g --jid "$jid" -- "$SAMPLES/$sample" "$gate"
    log=$(logfile_for_jid "$jid")
    wait_for_cmd 60 grep -q READY "$log"

    timeout 180 cedana dump job "$jid"

    touch "$gate"
    run timeout 180 cedana restore job "$jid" --attach
    rm -f "$gate"
    assert_success
    assert_output --partial PASS
}

# XXX: remove once cedana-gpu #309 (CED-2507) is in the GPU plugin; these hang until then
require_reset_fix() {
    skip "known bug: CUDA applications that reset the device hang under the interceptor (cedana-gpu #309)"
}

# A device check at startup that resets the device, then work, as GROMACS does
# bats test_tags=run,reset
@test "[$GPU_INFO] run GPU process using the device after cudaDeviceReset" {
    require_reset_fix
    run_sample reset_stream
}

# The runtime's destructors run after a reset at exit
# bats test_tags=run,reset
@test "[$GPU_INFO] run GPU process resetting the device before it exits" {
    require_reset_fix
    run_sample reset_exit
}

# Reset at startup, checkpointed mid-work, restored, then reset again before it exits
# bats test_tags=restore,reset
@test "[$GPU_INFO] restore GPU process that resets the device before and after the checkpoint" {
    require_reset_fix
    restore_sample reset_exit
}

# A main process that isn't CUDA, which runs a short-lived CUDA probe at startup (e.g. from
# BASH_ENV): the probe exiting must not take the job down, and the job must still checkpoint
# and restore (customer issue P-17)
# bats test_tags=restore,probe
@test "[$GPU_INFO] restore GPU job after a short-lived CUDA probe in it" {
    require_sample gpu_props
    # XXX: remove once fixed, this test found it
    skip "known bug: when the job's only CUDA process exits, the GPU controller shuts down and signals the job's whole process group (SIGUSR2, exit 12)"

    local jid log
    jid=$(unix_nano)
    SAMPLE_JID=$jid # for teardown
    cedana run process -g --jid "$jid" -- sh -c "$SAMPLES/gpu_props >/dev/null && echo PROBED; while true; do echo alive; sleep 0.5; done"
    log=$(logfile_for_jid "$jid")
    wait_for_cmd 60 grep -q PROBED "$log"
    sleep 2

    run bats_pipe cedana ps \| grep "$jid"
    assert_success
    refute_output --partial "halted"

    cedana dump job "$jid"
    local lines
    lines=$(wc -l <"$log")
    cedana restore job "$jid"
    wait_for_cmd 30 "tail -n +$((lines + 1)) $log | grep -q alive"

    run cedana job kill "$jid"
}

# A CUDA process started after the restore, by a restored process that isn't CUDA itself
# (customer issue P-34)
# bats test_tags=restore,child
@test "[$GPU_INFO] restore GPU job, then start a CUDA child process in it" {
    require_sample gpu_props
    # XXX: remove once fixed, this test found it
    skip "known bug: a CUDA process started after the restore hangs, as does the controller's restore (it never completes once the process attaches)"

    local jid gate
    jid=$(unix_nano)
    SAMPLE_JID=$jid # for teardown
    gate="/tmp/cedana-gate-$jid"
    cedana run process -g --jid "$jid" -- sh -c "while [ ! -f $gate ]; do sleep 0.2; done; $SAMPLES/gpu_props; s=\$?; echo CHILD_EXIT=\$s; exit \$s"
    sleep 1

    cedana dump job "$jid"

    touch "$gate"
    run timeout 180 cedana restore job "$jid" --attach
    rm -f "$gate"
    assert_success
    assert_output --partial "Device name"
    assert_output --partial "CHILD_EXIT=0"
}

# Pinned host memory (cudaHostRegister, from malloc and MAP_SHARED) that kernels use zero-copy
# (customer issues P-14, P-43)
# bats test_tags=run,hostregister
@test "[$GPU_INFO] run GPU process using registered host memory zero-copy" {
    run_sample host_register
}

# bats test_tags=restore,hostregister
@test "[$GPU_INFO] restore GPU process using registered host memory zero-copy" {
    restore_sample host_register
}

# Two processes sharing device memory and events over CUDA IPC (as tensor-parallel ranks and
# NCCL do); after the restore, fresh exports must not reuse a handle from before it
# bats test_tags=run,ipc
@test "[$GPU_INFO] run GPU processes sharing memory over CUDA IPC" {
    # XXX: remove once fixed, this test found it
    skip "known bug: under the interceptor, cudaIpcGetEventHandle fails with 34 (CUDA driver is a stub library)"
    run_sample cuda_ipc
}

# bats test_tags=restore,ipc
@test "[$GPU_INFO] restore GPU processes sharing memory over CUDA IPC" {
    # XXX: remove once fixed, this test found it
    skip "known bug: under the interceptor, cudaIpcGetEventHandle fails with 34 (CUDA driver is a stub library)"
    restore_sample cuda_ipc
}

# Two processes sharing VMM allocations through exported file descriptors sent over a unix
# socket, as NCCL does with cuMem buffers; one fd closed after the import, the other kept open
# bats test_tags=run,ipc
@test "[$GPU_INFO] run GPU processes sharing VMM memory as exported fds" {
    run_sample cumem_ipc_fd
}

# bats test_tags=restore,ipc
@test "[$GPU_INFO] restore GPU processes sharing VMM memory as exported fds" {
    restore_sample cumem_ipc_fd
}

# Async copies between pinned host memory and reallocated device buffers, still in flight at
# the checkpoint (a stale device pointer replayed, cedana-gpu #288)
# bats test_tags=run,memcpy
@test "[$GPU_INFO] run GPU process copying async from pinned memory" {
    run_sample memcpy_async_pinned
}

# bats test_tags=restore,memcpy
@test "[$GPU_INFO] restore GPU process with async copies from pinned memory in flight" {
    restore_sample memcpy_async_pinned
}

# Event records posted asynchronously on one thread must still order a wait or a query on
# another (CED-2567: stale cuStreamWaitEvent, early cuEventQuery under the launch ring)
# bats test_tags=run,events
@test "[$GPU_INFO] run GPU process ordering events across threads" {
    run_sample event_race
}

# bats test_tags=restore,events
@test "[$GPU_INFO] restore GPU process ordering events across threads" {
    restore_sample event_race
}

# Launches posted asynchronously on one thread must still run ahead of work another thread
# queues on the same stream, and must not be lost when the thread exits (CED-2567)
# bats test_tags=run,streams
@test "[$GPU_INFO] run GPU process ordering a shared stream across threads" {
    run_sample stream_race
}

# bats test_tags=restore,streams
@test "[$GPU_INFO] restore GPU process ordering a shared stream across threads" {
    restore_sample stream_race
}

# vLLM's CPU KV-cache offload (SimpleCPUOffloadConnector) with one CPU tensor on the brk heap,
# replayed as the CUDA calls vLLM makes, without vLLM (CED-2563: a customer's prefill worker
# failed at startup, its 0x46f78-byte tensor refused by cuMemHostRegister):
#   vLLM: torch.zeros(...) + pin_tensor -> cudaHostRegister(ptr, nbytes, 0), raising on failure
#   here: posix_memalign(64) + memset + the same call; one region served from the heap's top
#         chunk, the others mmap'd (registered unpinned and pinned, respectively)
#   vLLM: DmaCopyBackend, one cuMemcpyBatchAsync (12.8 entry point) per transfer from a copy
#         thread, low-priority streams, stores waiting on a compute-done event, events polled
#   here: the same, every block checked after each step, live heap neighbours checked throughout
# bats test_tags=run,hostregister,memcpy
@test "[$GPU_INFO] run GPU process offloading KV cache like vLLM, one CPU tensor on the brk heap" {
    run_sample kv_offload_heap
}

# The same, checkpointed with transfers in flight; after the restore every CPU and GPU block must
# be exact and the offload keep working, and a new heap registration must work with the new GPU
# controller
# bats test_tags=restore,hostregister,memcpy
@test "[$GPU_INFO] restore GPU process offloading KV cache like vLLM, one CPU tensor on the brk heap" {
    restore_sample kv_offload_heap
}

# __device__ and __constant__ globals, by symbol and by address (globals moved, CED-2487)
# bats test_tags=run,globals
@test "[$GPU_INFO] run GPU process using device globals" {
    run_sample device_globals
}

# bats test_tags=restore,globals
@test "[$GPU_INFO] restore GPU process using device globals" {
    # XXX: remove once fixed, this test found it
    skip "known bug: after the restore, cudaMemcpyFromSymbol fails with 13 (invalid device symbol)"
    restore_sample device_globals
}

# Driver-API modules JIT-compiled from PTX, one image loaded twice (images shared, #305)
# bats test_tags=run,modules
@test "[$GPU_INFO] run GPU process with JIT-compiled driver API modules" {
    # XXX: remove once fixed, this test found it
    skip "known bug: under the interceptor, cuModuleLoadDataEx fails with 34 (CUDA driver is a stub library)"
    run_sample jit_modules
}

# bats test_tags=restore,modules
@test "[$GPU_INFO] restore GPU process with JIT-compiled driver API modules" {
    # XXX: remove once fixed, this test found it
    skip "known bug: under the interceptor, cuModuleLoadDataEx fails with 34 (CUDA driver is a stub library)"
    restore_sample jit_modules
}

# Modules whose image file is replaced by rename after they're loaded, by path and from a
# mapping, as when a late rank's JIT rewrites a file other ranks loaded (CED-2400). Each must keep
# running the image it was loaded from, and a later load of the path the new one
# bats test_tags=run,jit
@test "[$GPU_INFO] run GPU process with modules whose files are replaced" {
    run_sample replaced_module
}

# bats test_tags=restore,jit
@test "[$GPU_INFO] restore GPU process with modules whose files are replaced" {
    restore_sample replaced_module
}

# VMM mappings (remapped, aliased) and stream-ordered allocation pools
# bats test_tags=run,vmm
@test "[$GPU_INFO] run GPU process using VMM and memory pools" {
    # XXX: remove once fixed, this test found it
    skip "known bug: under the interceptor, cudaMemPoolGetAttribute fails with 34 (CUDA driver is a stub library)"
    run_sample vmm_pools
}

# bats test_tags=restore,vmm
@test "[$GPU_INFO] restore GPU process using VMM and memory pools" {
    # XXX: remove once fixed, this test found it
    skip "known bug: under the interceptor, cudaMemPoolGetAttribute fails with 34 (CUDA driver is a stub library)"
    restore_sample vmm_pools
}
