#!/usr/bin/env bash

# Helper functions that hit the local Cedana API

exec_task() {
    local task="$1"
    local job_id="$2"
    cedana exec -w "$PWD" "$task" -i "$job_id"
}

checkpoint_task() {
    local job_id="$1"
    cedana dump job "$job_id" -d /tmp
}

restore_task() {
    local job_id="$1"
    cedana restore job "$job_id"
}
