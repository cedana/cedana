#!/usr/bin/env bats

# A GPU job whose main process is a shell script that outlives its CUDA child.
#
# The GPU controller shuts down when its last worker exits, and a worker exits
# when it sees its CUDA process end, by a 500 ms poll. Up to cedana-gpu v0.8.7
# the controller then sent SIGUSR2, its premature-exit signal, to the attached
# process if it was still alive. A bash script with an EXIT trap runs the trap
# and dies of the signal after all its work, and cedana reports exit code 255
# (CED-2541). The script here sleeps 1 s after its CUDA child, longer than the
# poll, so the controller always sees the child end while the script lives.
#
# bats file_tags=gpu

load ../../helpers/utils
load ../../helpers/daemon
load ../../helpers/gpu

load_lib support
load_lib assert
load_lib file

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
    teardown_daemon
    check_gpu_signals
}

teardown_file() {
    teardown_file_daemon
    check_gpu_signals
}

# Writes a script with an EXIT trap that runs a CUDA sample and then sleeps
# longer than the worker's poll of its process. Prints the script's path.
write_script() {
    local script="$BATS_TEST_TMPDIR/job.sh"
    cat >"$script" <<'SCRIPT'
trap 'echo "END rc=$?"' EXIT
/cedana-samples/gpu_smr/mem-throughput-saxpy
sleep 1
SCRIPT
    echo "$script"
}

# bats test_tags=serverless
@test "[$GPU_INFO] run GPU script that outlives its CUDA child (without daemon)" {
    jid=$(unix_nano)
    script=$(write_script)

    run cedana run process --no-server -g --jid "$jid" -- /bin/bash "$script"
    assert_success
    assert_output --partial "END rc=0"
}

@test "[$GPU_INFO] run GPU script that outlives its CUDA child" {
    jid=$(unix_nano)
    script=$(write_script)

    run cedana run process --attach -g --jid "$jid" -- /bin/bash "$script"
    assert_success
    assert_output --partial "END rc=0"
}
