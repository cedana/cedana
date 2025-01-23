#!/usr/bin/env bats

# This file assumes its being run from the same directory as the Makefile

load ../helpers/utils
load ../helpers/daemon

load_lib support
load_lib assert
load_lib file

############
### Dump ###
############

# @test "stream dump process" {
#     "$WORKLOADS"/date-loop.sh &
#     pid=$!

#     run cedana dump process $pid --stream 1
#     assert_success

#     dump_file=$(echo "$output" | awk '{print $NF}')
#     assert_exists "$dump_file"

#     run kill $pid
# }

# @test "stream dump process (custom name)" {
#     "$WORKLOADS"/date-loop.sh &
#     pid=$!
#     name=$(unix_nano)

#     run cedana dump process $pid --name "$name" --dir /tmp --stream 1
#     assert_success

#     assert_exists "/tmp/$name"

#     run kill $pid
# }

# @test "dump process (tar compression)" {
#     "$WORKLOADS"/date-loop.sh &
#     pid=$!
#     name=$(unix_nano)

#     run cedana dump process $pid --name "$name" --dir /tmp --stream --compression tar
#     assert_success

#     assert_exists "/tmp/$name"
#     assert_exists "/tmp/$name/img-0.lz4" # XXX: Will just override to lz4 until we move compression from streamer to the daemon

#     run kill $pid
# }

# @test "dump process (gzip compression)" {
#     "$WORKLOADS"/date-loop.sh &
#     pid=$!
#     name=$(unix_nano)

#     run cedana dump process $pid --name "$name" --dir /tmp --stream --compression gzip
#     assert_success

#     assert_exists "/tmp/$name.tar.gz"
#     assert_exists "/tmp/$name/img-0.lz4" # XXX: Will just override to lz4 until we move compression from streamer to the daemon

#     run kill $pid
# }

# @test "dump process (lz4 compression)" {
#     "$WORKLOADS"/date-loop.sh &
#     pid=$!
#     name=$(unix_nano)

#     run cedana dump process $pid --name "$name" --dir /tmp --stream --compression lz4
#     assert_success

#     assert_exists "/tmp/$name.tar.lz4"

#     run kill $pid
# }

# @test "dump process (invalid compression)" {
#     "$WORKLOADS"/date-loop.sh &
#     pid=$!
#     name=$(unix_nano)

#     run cedana dump process $pid --name "$name" --dir /tmp --stream --compression jibberish
#     assert_failure

#     assert_not_exists "/tmp/$name"
#     assert_not_exists "/tmp/$name/img-0.lz4"

#     run kill $pid
# }

# ############
# ### Dump ###
# ############
