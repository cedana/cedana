#!/usr/bin/env bats

# A process that creates and destroys CUDA texture objects, and reads them in kernels with the
# handle as a plain parameter and inside a struct, as GROMACS does. Skips if the samples image
# predates the binary.
#
# bats file_tags=gpu,texture

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget

TEX_OBJECT=/cedana-samples/gpu_smr/tex_object

setup_file() {
    if ! cmd_exists nvidia-smi; then
        skip "GPU not available"
    fi
    [ -x "$TEX_OBJECT" ] || skip "tex_object not in image (rebuild cedana-samples image)"
    setup_file_daemon
}

setup() {
    setup_daemon
}

teardown() {
    # A failed dump_restore_texture stops before its own cleanup, and the sample waits on its
    # gate with the GPU in use until it is killed.
    if [ -n "${gate:-}" ]; then
        rm -f "$gate"
        cedana job kill "$jid" &>/dev/null || true
        unset gate
    fi
    teardown_daemon
}

teardown_file() {
    teardown_file_daemon
}

# Runs the sample with a gate file, dumps it after READY, restores it, opens the gate and
# checks that every read through the restored texture objects is right. Args: extra sample flags.
dump_restore_texture() {
    jid=$(unix_nano)
    gate=/tmp/gate-$jid
    rm -f "$gate"

    cedana run process -g --jid "$jid" -- "$TEX_OBJECT" "$@" "$gate"
    log_file=$(logfile_for_jid "$jid")
    watch_logs "$jid"

    wait_for_cmd 60 grep -q READY "$log_file" # the live texture objects exist and read right

    cedana dump job "$jid"

    cedana restore job "$jid"
    log_file=$(logfile_for_jid "$jid")
    watch_logs "$jid"

    sleep 1
    touch "$gate" # finish the reads on the restored texture objects

    wait_for_cmd 60 grep -q PASS "$log_file"
    run grep -E "MISMATCH|FAIL" "$log_file"
    assert_failure
}

###########
### Run ###
###########

# cuTexObjectDestroy reaches the driver: the GPU interceptor answered it with
# CUDA_ERROR_STUB_LIBRARY (34) before CED-2543.
@test "[$GPU_INFO] run GPU process (texture objects destroyed and created again)" {
    jid=$(unix_nano)

    run cedana run process --attach -g --jid "$jid" -- "$TEX_OBJECT"
    assert_success
    assert_output --partial "PASS"
}

@test "[$GPU_INFO] run GPU process (texture objects, none destroyed before the end)" {
    jid=$(unix_nano)

    run cedana run process --attach -g --jid "$jid" -- "$TEX_OBJECT" --control
    assert_success
    assert_output --partial "PASS"
}

###############
### Restore ###
###############

# The control: no texture object is destroyed before the dump.
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (texture objects, none destroyed before the end)" {
    dump_restore_texture --control
}

# The checkpoint holds destroyed texture objects next to the live ones. Each live object must
# get its old handle back at the restore, because the kernels take the handle by value.
# bats test_tags=restore
@test "[$GPU_INFO] restore GPU process (texture objects destroyed and created again)" {
    dump_restore_texture
}
