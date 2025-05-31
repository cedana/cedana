#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile
#
# bats file_tags=gpu,large

load ../helpers/utils
load ../helpers/daemon
load ../helpers/gpu

load_lib support
load_lib assert
load_lib file

export CEDANA_CHECKPOINT_COMPRESSION=gzip # To avoid blowing up storage budget

setup_file() {
    setup_file_daemon
    if cmd_exists nvidia-smi; then
        do_once install_requirements
        do_once download_hf_models
    fi

}

#####################
### Inference C/R ###
#####################

# bats test_tags=dump,restore
@test "c/r transformers inference workload - stabilityai/stablelm-2-1_6b" {
    # Requires an HF token!
    run_inference_test "stabilityai/stablelm-2-1_6b"
}
