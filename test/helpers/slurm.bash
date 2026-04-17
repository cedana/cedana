#!/usr/bin/env bash

################################
# SLURM Job Management & C/R  #
################################

# Source setup helpers (shared vars + functions)
source "$(dirname "${BASH_SOURCE[0]}")/slurm_setup.bash"

slurm_submit_job() {
    local sbatch_file="$1"
    local rel_path container_dir container_file

    rel_path="${sbatch_file#*/slurm/}"
    container_dir="/data/cedana-samples/slurm/$(dirname "$rel_path")"
    container_file="$(basename "$rel_path")"
    info_log "Submitting: cd $container_dir && sbatch $container_file"

    local output exit_code
    output=$(slurm_exec bash -c \
        "cd '$container_dir' && sbatch --parsable --overcommit \
         --export=ALL,PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
         --cpus-per-task=1 --mem=0 '$container_file'" 2>&1)
    exit_code=$?

    if [ "$exit_code" -ne 0 ]; then
        error_log "sbatch failed: $output"
        return 1
    fi

    local job_id
    job_id=$(echo "$output" | tail -1 | cut -d';' -f1 | tr -d '[:space:]')
    info_log "Submitted $container_file -> job $job_id"
    echo "$job_id"
}

_dump_job_failure_info() {
    local job_id="${1:-}"

    echo "=== sacct (last 10 jobs) ==="
    slurm_exec sacct --noheader -a \
        --format=JobID,JobName,State,ExitCode,DerivedExitCode,Reason,NodeList,Submit,Start,End \
        -P 2>/dev/null | tail -10 || true

    if [ -n "$job_id" ]; then
        echo "=== scontrol show job $job_id ==="
        slurm_exec scontrol show job "$job_id" 2>/dev/null || true

        echo "=== job output files ==="
        for c in $(_slurm_compute_containers); do
            for f in $(docker exec "$c" \
                find "${SLURM_DATA_DIR}" -name "*-${job_id}.*" 2>/dev/null); do
                echo "--- $c:$f ---"
                docker exec "$c" cat "$f" 2>/dev/null || true
            done
        done
    fi

    echo "=== slurmctld.log (last 50 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" \
        tail -50 /var/log/slurm/slurmctld.log 2>/dev/null || true

    echo "=== slurmd.log on compute nodes (last 50 lines) ==="
    for c in $(_slurm_compute_containers); do
        echo "--- $c ---"
        docker exec "$c" tail -50 /var/log/slurm/slurmd.log 2>/dev/null ||
            echo "(unavailable)"
    done

    echo "=== cedana daemon log on compute nodes (last 30 lines) ==="
    for c in $(_slurm_compute_containers); do
        echo "--- $c ---"
        docker exec "$c" tail -30 /var/log/cedana.log 2>/dev/null ||
            echo "(no log)"
    done

    echo "=== cedana-slurm status/log on compute nodes ==="
    for c in $(_slurm_compute_containers); do
        echo "--- $c processes ---"
        docker exec "$c" pgrep -fa 'cedana-slurm' 2>/dev/null ||
            echo "(no cedana-slurm processes)"
        echo "--- $c cedana-slurm log (last 80 lines) ---"
        docker exec "$c" tail -80 /var/log/cedana-slurm.log 2>/dev/null ||
            echo "(no log)"
        echo "--- $c cedana-slurm monitor log (last 120 lines) ---"
        docker exec "$c" tail -120 /var/log/cedana-slurm-monitor.log 2>/dev/null ||
            echo "(no monitor log)"
    done

    echo "=== cedana-slurm log on controller (last 50 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" \
        tail -50 /var/log/cedana-slurm.log 2>/dev/null || true

    echo "=== cedana-slurm monitor log on controller (last 120 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" \
        tail -120 /var/log/cedana-slurm-monitor.log 2>/dev/null || true
}

wait_for_slurm_job_state() {
    local job_id="$1"
    local target_state="$2"
    local timeout="${3:-60}"
    local elapsed=0

    while [ "$elapsed" -lt "$timeout" ]; do
        local state
        state=$(slurm_exec scontrol show job "$job_id" 2>/dev/null |
            grep -oP 'JobState=\K\S+' || echo "UNKNOWN")

        info_log "Job $job_id state: $state (want: $target_state)"

        [ "$state" = "$target_state" ] && return 0

        case "$state" in
        COMPLETED | FAILED | CANCELLED | TIMEOUT | NODE_FAIL)
            error_log "Job $job_id reached terminal state $state (expected $target_state)"
            _dump_job_failure_info "$job_id"
            return 1
            ;;
        esac

        sleep 2
        elapsed=$((elapsed + 2))
    done

    error_log "Timeout: job $job_id did not reach $target_state after ${timeout}s"
    return 1
}

cancel_slurm_job() {
    slurm_exec scancel "$1" 2>/dev/null || true
}

##############################
# C/R Test Orchestrator
##############################

test_slurm_job() {
    local action_sequence="$1"
    local sbatch_file="$2"
    local dump_wait_time="${3:-10}"
    local dump_timeout="${4:-120}"

    IFS='_' read -ra actions <<<"$action_sequence"

    info_log "Starting SLURM action sequence: $action_sequence (file=$sbatch_file, dump_wait=${dump_wait_time}s, dump_timeout=${dump_timeout}s)"

    local job_id="" action_id="" submitted=false error=""

    for action in "${actions[@]}"; do
        case "$action" in
        SUBMIT)
            [ "$submitted" = true ] && {
                error="Cannot SUBMIT twice"
                break
            }

            info_log "Submitting job from $sbatch_file..."
            job_id=$(slurm_submit_job "$sbatch_file") ||
                {
                    error="Failed to submit job"
                    break
                }

            wait_for_slurm_job_state "$job_id" "RUNNING" 60 ||
                {
                    error="Job $job_id failed to reach RUNNING"
                    break
                }

            info_log "Job $job_id running — waiting ${dump_wait_time}s before dump..."
            sleep "$dump_wait_time"
            submitted=true
            ;;

        DUMP)
            [ "$submitted" = false ] && {
                error="Cannot DUMP — no job submitted"
                break
            }
            [ -z "$job_id" ] && {
                error="Cannot DUMP — no active job ID"
                break
            }

            local _host
            _host=$(slurm_exec scontrol show job "$job_id" 2>/dev/null | grep -oP 'BatchHost=\K\S+' | head -1)
            if [ -n "$_host" ]; then
                info_log "[DEBUG] SPANK monitor check on $_host for job $job_id:"
                docker exec "$_host" bash -c "ps -eo pid,ppid,stat,cmd | grep -E '[c]edana-slurm monitor'" 2>/dev/null || info_log "[DEBUG] No monitor process found"
                info_log "[DEBUG] slurmd PATH:"
                docker exec "$_host" bash -c "cat /proc/\$(pgrep -x slurmd | head -1)/environ 2>/dev/null | tr '\0' '\n' | grep ^PATH" 2>/dev/null || info_log "[DEBUG] Could not read slurmd environ"
                info_log "[DEBUG] SPANK log entries:"
                docker exec "$_host" bash -c "for f in /var/log/cedana-slurm.log /var/log/cedana-slurm-monitor.log; do [ -f \"\$f\" ] || continue; echo \"--- \$f ---\"; grep -i 'spank\|monitor\|checkpoint request\|checkpoint consumer\|failed to get event stream\|failed to setup checkpoint request consumer\|failed to connect to rabbitmq\|checkpoint failed for job ID\|publishing checkpoint info' \"\$f\" | tail -15; done" 2>/dev/null || info_log "[DEBUG] No SPANK/monitor entries in logs"
            fi

            info_log "Checkpointing SLURM job $job_id via propagator..."
            local checkpoint_output checkpoint_status

            checkpoint_output=$(checkpoint_slurm_job "$job_id")
            checkpoint_status=$?
            [ "$checkpoint_status" -ne 0 ] && {
                error="Checkpoint failed: $checkpoint_output"
                break
            }

            action_id="$checkpoint_output"
            validate_action_id "$action_id" ||
                {
                    error="Invalid action ID: $action_id"
                    break
                }

            info_log "Checkpoint request returned action_id=$action_id"

            if [ -n "$_host" ]; then
                sleep 3
                info_log "[DEBUG] Monitor status after checkpoint request:"
                docker exec "$_host" bash -c "ps -eo pid,ppid,stat,cmd | grep -E '[c]edana-slurm monitor'" 2>/dev/null || info_log "[DEBUG] Monitor DIED after checkpoint request"
                info_log "[DEBUG] Last 120 lines of cedana-slurm logs:"
                docker exec "$_host" bash -c "for f in /var/log/cedana-slurm.log /var/log/cedana-slurm-monitor.log; do [ -f \"\$f\" ] || continue; echo \"--- \$f ---\"; tail -120 \"\$f\"; done" 2>/dev/null || true
            fi

            poll_slurm_action_status "$action_id" "checkpoint" "$dump_timeout" ||
                {
                    error="Checkpoint action $action_id did not complete"
                    break
                }

            info_log "Checkpoint complete (action_id: $action_id)"
            ;;

        RESTORE)
            [ -z "$action_id" ] && {
                error="Cannot RESTORE — no checkpoint action ID"
                break
            }

            info_log "Cancelling job $job_id before restore..."
            cancel_slurm_job "$job_id"
            sleep 2

            info_log "Restoring job from action $action_id..."
            local restore_output restore_status restore_action_id
            restore_output=$(restore_slurm_job "$action_id" "$SLURM_CLUSTER_ID")
            restore_status=$?
            [ "$restore_status" -ne 0 ] && {
                error="Restore failed: $restore_output"
                break
            }

            restore_action_id="$restore_output"
            validate_action_id "$restore_action_id" ||
                {
                    error="Invalid restore action ID: $restore_action_id"
                    break
                }

            info_log "Restore request returned action_id=$restore_action_id"

            info_log "Waiting for restored job to appear..."
            sleep 5

            local new_job_id
            new_job_id=$(slurm_exec squeue -h -o '%i' --sort=-V 2>/dev/null | head -1)
            if [ -n "$new_job_id" ] && [ "$new_job_id" != "$job_id" ]; then
                job_id="$new_job_id"
                info_log "Restored job has new ID: $job_id"
            else
                info_log "No new restored job ID detected; continuing with job_id=$job_id"
            fi

            wait_for_slurm_job_state "$job_id" "RUNNING" 60 ||
                {
                    error="Restored job $job_id failed to reach RUNNING"
                    break
                }

            info_log "Restored job $job_id is running"
            submitted=true
            ;;

        *)
            error="Unknown action: $action"
            break
            ;;
        esac
    done

    [ -n "$job_id" ] && cancel_slurm_job "$job_id"
    [ -n "$job_id" ] && info_log "Cleanup: cancelled job $job_id"

    if [ -n "$error" ]; then
        error_log "$error"
        slurm_exec squeue 2>/dev/null || true
        slurm_exec sinfo 2>/dev/null || true
        _dump_job_failure_info "${job_id:-}"
        return 1
    fi

    return 0
}
