#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

###########
### Run ###
###########

@test "run GPU process (dummy)" {
    jid=$(unix_nano)
    log_file="/var/log/cedana-output-$jid.log"

    run cedana run process -g --jid "$jid" -- echo hello

    assert_success
    assert_exists "$log_file"
    assert_file_contains "$log_file" "hello"

    run cedana ps

    assert_success
    assert_output --partial "$jid"
}
