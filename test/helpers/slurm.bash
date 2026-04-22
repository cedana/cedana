#!/usr/bin/env bash

################################
# SLURM Job Management & C/R  #
################################

# Source setup helpers (shared vars + functions)
source "$(dirname "${BASH_SOURCE[0]}")/slurm_setup.bash"

slurm_submit_job() {
    local sbatch_file="$1"
    local rel_path container_dir container_file
    local cedana_enable="${CEDANA_ENABLE:-1}"

    rel_path="${sbatch_file#*/slurm/}"
    container_dir="/data/cedana-samples/slurm/$(dirname "$rel_path")"
    container_file="$(basename "$rel_path")"
    info_log "Submitting: cd $container_dir && sbatch $container_file"

    local output
    if ! output=$(slurm_exec bash -c \
        "cd '$container_dir' && sbatch --parsable --overcommit \
         --export=ALL,CEDANA_ENABLE=${cedana_enable},PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
         --cpus-per-task=1 --mem=0 '$container_file'" 2>&1); then
        error_log "sbatch failed: $output"
        return 1
    fi

    local job_id
    job_id=$(echo "$output" | tail -1 | cut -d';' -f1 | tr -d '[:space:]')
    info_log "Submitted $container_file -> job $job_id"
    echo "$job_id"
}

_slurm_sample_container_dir() {
    local sbatch_file="$1"
    local rel_path

    rel_path="${sbatch_file#*/slurm/}"
    printf '/data/cedana-samples/slurm/%s\n' "$(dirname "$rel_path")"
}

_slurm_relevant_job_ids_csv() {
    local joined=""
    local id

    for id in "$@"; do
        [ -z "$id" ] && continue
        if [ -n "$joined" ]; then
            joined+=","
        fi
        joined+="$id"
    done

    printf '%s\n' "$joined"
}

_persist_container_log_file() {
    local container="$1"
    local src="$2"
    local dst_dir="$3"
    local base

    base="$(basename "$src")"
    mkdir -p "$dst_dir"

    if docker exec "$container" test -f "$src" 2>/dev/null; then
        docker cp "$container:$src" "$dst_dir/$base" 2>/dev/null ||
            docker exec "$container" sh -c "cat '$src'" >"$dst_dir/$base" 2>&1 || true
    fi
}

_capture_runtime_slurm_logs() {
    local reason="${1:-unknown}"
    local job_id="${2:-unknown}"
    local host_hint="${3:-unknown}"
    local sample_dir="${4:-/data/cedana-samples}"
    local relevant_job_ids_csv="${5:-$job_id}"
    local stamp out_root out_dir
    local containers=()

    stamp="$(date -u +%Y%m%dT%H%M%SZ)"
    out_root="${SLURM_RUNTIME_DEBUG_DIR:-/tmp/slurm-runtime-debug}"
    out_dir="$out_root/${stamp}_${reason}_job-${job_id}"

    mkdir -p "$out_dir"
    {
        echo "timestamp_utc=$stamp"
        echo "reason=$reason"
        echo "job_id=$job_id"
        echo "host_hint=$host_hint"
        echo "sample_dir=$sample_dir"
        echo "relevant_job_ids=$relevant_job_ids_csv"
    } >"$out_dir/context.txt"

    docker ps -a >"$out_dir/docker-ps-a.txt" 2>&1 || true
    docker network ls >"$out_dir/docker-network-ls.txt" 2>&1 || true

    mapfile -t containers < <(
        {
            [ -n "${SLURM_CONTROLLER_CONTAINER:-}" ] && echo "$SLURM_CONTROLLER_CONTAINER"
            _slurm_compute_containers
        } | awk 'NF && !seen[$0]++'
    )

    if [ "${#containers[@]}" -eq 0 ]; then
        echo "No slurm containers found while capturing runtime logs" >"$out_dir/no-slurm-containers.txt"
    fi

    for c in "${containers[@]}"; do
        [ -z "$c" ] && continue
        cdir="$out_dir/$c"
        mkdir -p "$cdir"

        docker inspect "$c" >"$cdir/inspect.json" 2>&1 || true
        docker logs "$c" >"$cdir/docker-logs.txt" 2>&1 || true
        docker exec "$c" sh -c 'ps auxww' >"$cdir/processes.txt" 2>&1 || true
        docker exec "$c" sh -c 'for p in $(pgrep -f "cedana-slurm monitor" 2>/dev/null || true); do echo "=== monitor pid=$p ==="; tr "\000" "\n" </proc/$p/environ 2>/dev/null | sort; done' >"$cdir/monitor-environ.txt" 2>&1 || true
        docker exec "$c" sh -c 'for f in /usr/local/bin/cedana /usr/local/bin/cedana-slurm /usr/local/lib/libcedana-storage-cedana.so /usr/local/lib/libcedana-storage-s3.so /usr/local/lib/libcedana-runc.so /usr/local/lib/libcedana-slurm.so; do [ -f "$f" ] || continue; echo "=== $f ==="; ls -l "$f"; sha256sum "$f"; done' >"$cdir/binary-sha256.txt" 2>&1 || true
        docker exec "$c" sh -c 'if command -v go >/dev/null 2>&1; then for f in /usr/local/bin/cedana /usr/local/bin/cedana-slurm /usr/local/lib/libcedana-storage-cedana.so /usr/local/lib/libcedana-storage-s3.so /usr/local/lib/libcedana-runc.so /usr/local/lib/libcedana-slurm.so; do [ -f "$f" ] || continue; echo "=== $f ==="; go version -m "$f" || true; done; else echo "go command unavailable in container"; fi' >"$cdir/go-version-m.txt" 2>&1 || true
        docker exec "$c" sh -c 'echo "=== /usr/local/lib plugins ==="; ls -la /usr/local/lib/libcedana-*.so 2>/dev/null || true; echo "=== CEDANA_* env ==="; env | sort | grep "^CEDANA_" || true' >"$cdir/plugin-inventory.txt" 2>&1 || true

        _persist_container_log_file "$c" /var/log/cedana.log "$cdir"
        _persist_container_log_file "$c" /var/log/cedana-slurm.log "$cdir"
        _persist_container_log_file "$c" /var/log/cedana-slurm-monitor.log "$cdir"
        _persist_container_log_file "$c" /var/log/slurm/slurmctld.log "$cdir"
        _persist_container_log_file "$c" /var/log/slurm/slurmd.log "$cdir"
        _persist_container_log_file "$c" /var/log/slurm/slurmdbd.log "$cdir"
        _persist_container_log_file "$c" /etc/slurm/slurm.conf "$cdir"
        _persist_container_log_file "$c" /etc/slurm/gres.conf "$cdir"
        _persist_container_log_file "$c" /var/log/munge/munged.log "$cdir"

        if [ "${GPU:-0}" = "1" ]; then
            docker exec "$c" sh -c 'echo "=== nvidia-smi -L ==="; nvidia-smi -L 2>&1 || true; echo "=== /dev/nvidia* ==="; ls -la /dev/nvidia* 2>&1 || true; echo "=== /etc/slurm/gres.conf ==="; cat /etc/slurm/gres.conf 2>&1 || true; echo "=== /etc/slurm/slurm.conf (GPU lines) ==="; grep -E "^(NodeName|GresTypes|DebugFlags)" /etc/slurm/slurm.conf 2>&1 || true; echo "=== slurmd -C ==="; /usr/sbin/slurmd -C 2>&1 || true; echo "=== slurmd -G ==="; /usr/sbin/slurmd -G 2>&1 || true' >"$cdir/gpu-diagnostics.txt" 2>&1 || true
        fi

        docker exec "$c" sh -c 'squeue || true; sinfo || true; sacct -n -a -P || true' >"$cdir/slurm-snapshots.txt" 2>&1 || true
        docker exec \
            -e SLURM_DEBUG_SAMPLE_DIR="$sample_dir" \
            -e SLURM_DEBUG_JOB_IDS="$relevant_job_ids_csv" \
            "$c" sh -c '
                sample_dir="${SLURM_DEBUG_SAMPLE_DIR:-/data/cedana-samples}"
                ids="${SLURM_DEBUG_JOB_IDS:-}"
                if [ -d "$sample_dir" ]; then
                    IFS=","; for id in $ids; do
                        [ -n "$id" ] || continue
                        for suffix in out err; do
                            f="$sample_dir/slurm-$id.$suffix"
                            [ -f "$f" ] && printf "%s\n" "$f"
                        done
                    done
                    for f in "$sample_dir/.cedana_debug.out" "$sample_dir/.cedana_debug.err"; do
                        [ -f "$f" ] && printf "%s\n" "$f"
                    done
                fi
            ' >"$cdir/slurm-output-files.txt" 2>&1 || true

        while IFS= read -r job_out; do
            [ -z "$job_out" ] && continue
            out_name="$(echo "$job_out" | sed 's#^/##; s#/#_#g')"
            docker exec "$c" sh -c "cat '$job_out'" >"$cdir/${out_name}.txt" 2>&1 || true
        done <"$cdir/slurm-output-files.txt"
    done

    info_log "[DEBUG] Captured runtime SLURM logs to $out_dir"
}

_dump_job_failure_info() {
    local job_id="${1:-}"
    local sample_dir="${2:-/data/cedana-samples}"
    local relevant_job_ids_csv="${3:-$job_id}"

    _capture_runtime_slurm_logs "failure-dump" "$job_id" "unknown" "$sample_dir" "$relevant_job_ids_csv"

    echo "=== sacct (last 10 jobs) ==="
    slurm_exec sacct --noheader -a \
        --format=JobID,JobName,State,ExitCode,DerivedExitCode,Reason,NodeList,Submit,Start,End \
        -P 2>/dev/null | tail -10 || true

    if [ -n "$job_id" ]; then
        echo "=== scontrol show job $job_id ==="
        slurm_exec scontrol show job "$job_id" 2>/dev/null || true

        echo "=== job output files ==="
        for c in $(_slurm_compute_containers); do
            for f in $(docker exec \
                -e SLURM_DEBUG_SAMPLE_DIR="$sample_dir" \
                -e SLURM_DEBUG_JOB_IDS="$relevant_job_ids_csv" \
                "$c" sh -c '
                    sample_dir="${SLURM_DEBUG_SAMPLE_DIR:-/data/cedana-samples}"
                    ids="${SLURM_DEBUG_JOB_IDS:-}"
                    if [ -d "$sample_dir" ]; then
                        IFS=","; for id in $ids; do
                            [ -n "$id" ] || continue
                            for suffix in out err; do
                                f="$sample_dir/slurm-$id.$suffix"
                                [ -f "$f" ] && printf "%s\n" "$f"
                            done
                        done
                        for f in "$sample_dir/.cedana_debug.out" "$sample_dir/.cedana_debug.err"; do
                            [ -f "$f" ] && printf "%s\n" "$f"
                        done
                    fi
                ' 2>/dev/null | sort -u); do
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

_detect_restored_job_id() {
    local previous_job_id="$1"
    local previous_job_name="${2:-}"

    # Don't guess on bare job IDs. If we failed to capture the original name,
    # keep polling instead of attaching to an unrelated newer job.
    if [ -z "$previous_job_name" ]; then
        return 0
    fi

    local queued_job_id=""
    local squeue_out=""
    if squeue_out=$(slurm_exec squeue -h -o '%i|%j' --sort=-V 2>/dev/null); then
        queued_job_id=$(awk -F'|' -v prev="$previous_job_id" -v name="$previous_job_name" '$1 ~ /^[0-9]+$/ && ($1 + 0) > (prev + 0) && $2 == name { print $1; exit }' <<<"$squeue_out")
    fi
    if [ -n "$queued_job_id" ]; then
        echo "$queued_job_id"
        return 0
    fi

    local accounted_job_id=""
    local sacct_out=""
    if sacct_out=$(slurm_exec sacct --noheader -a --format=JobID,JobName -P 2>/dev/null); then
        accounted_job_id=$(awk -F'|' -v prev="$previous_job_id" -v name="$previous_job_name" '
            $1 ~ /^[0-9]+$/ && ($1 + 0) > (prev + 0) && $2 == name {
                if (max == "" || $1 + 0 > max + 0) {
                    max = $1
                }
            }
            END {
                if (max != "") {
                    print max
                }
            }' <<<"$sacct_out")
    fi

    [ -n "$accounted_job_id" ] && echo "$accounted_job_id"
    return 0
}

_get_slurm_job_name() {
    local job_id="$1"

    slurm_exec scontrol show job "$job_id" 2>/dev/null |
        grep -oP 'JobName=\K\S+' | head -1 || true
}

_get_batch_host() {
    local job_id="$1"

    slurm_exec scontrol show job "$job_id" 2>/dev/null |
        grep -oP 'BatchHost=\K\S+' | head -1 || true
}

wait_for_slurm_job_state() {
    local job_id="$1"
    local target_state="$2"
    local timeout="${3:-60}"
    local sample_dir="${4:-/data/cedana-samples}"
    local relevant_job_ids_csv="${5:-$job_id}"
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
            _dump_job_failure_info "$job_id" "$sample_dir" "$relevant_job_ids_csv"
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
    local sample_dir=""
    local relevant_job_ids_csv=""
    local tracked_job_ids=()

    IFS='_' read -ra actions <<<"$action_sequence"
    sample_dir="$(_slurm_sample_container_dir "$sbatch_file")"

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

            tracked_job_ids+=("$job_id")
            relevant_job_ids_csv="$(_slurm_relevant_job_ids_csv "${tracked_job_ids[@]}")"

            wait_for_slurm_job_state "$job_id" "RUNNING" 60 "$sample_dir" "$relevant_job_ids_csv" ||
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
            _host="$(_get_batch_host "$job_id")"
            if [ -n "$_host" ]; then
                info_log "[DEBUG] SPANK monitor check on $_host for job $job_id:"
                docker exec "$_host" bash -c "ps -eo pid,ppid,stat,cmd | grep -E '[c]edana-slurm monitor'" 2>/dev/null || info_log "[DEBUG] No monitor process found"
                info_log "[DEBUG] slurmd PATH:"
                docker exec "$_host" bash -c "cat /proc/\$(pgrep -x slurmd | head -1)/environ 2>/dev/null | tr '\0' '\n' | grep ^PATH" 2>/dev/null || info_log "[DEBUG] Could not read slurmd environ"
                info_log "[DEBUG] SPANK log entries:"
                docker exec "$_host" bash -c "for f in /var/log/cedana-slurm.log /var/log/cedana-slurm-monitor.log; do [ -f \"\$f\" ] || continue; echo \"--- \$f ---\"; grep -i 'spank\|monitor\|checkpoint request\|checkpoint consumer\|failed to get event stream\|failed to setup checkpoint request consumer\|failed to connect to rabbitmq\|checkpoint failed for job ID\|publishing checkpoint info' \"\$f\" | tail -15; done" 2>/dev/null || info_log "[DEBUG] No SPANK/monitor entries in logs"
            fi

            info_log "Checkpointing SLURM job $job_id via propagator..."
            local checkpoint_output

            if ! checkpoint_output=$(checkpoint_slurm_job "$job_id"); then
                error="Checkpoint failed: $checkpoint_output"
                break
            fi

            action_id="$checkpoint_output"
            validate_action_id "$action_id" ||
                {
                    error="Invalid action ID: $action_id"
                    break
                }

            info_log "Checkpoint request returned action_id=$action_id"

            if [ -n "$_host" ]; then
                local monitor_alive=true
                sleep 3
                info_log "[DEBUG] Monitor status after checkpoint request:"
                docker exec "$_host" bash -c "ps -eo pid,ppid,stat,cmd | grep -E '[c]edana-slurm monitor'" 2>/dev/null || {
                    monitor_alive=false
                    info_log "[DEBUG] Monitor DIED after checkpoint request"
                }
                info_log "[DEBUG] cedana-slurm log excerpts scoped to job $job_id:"
                docker exec "$_host" bash -c "for f in /var/log/cedana-slurm.log /var/log/cedana-slurm-monitor.log; do [ -f \"\$f\" ] || continue; echo \"--- \$f (job/action scoped) ---\"; grep -E 'jobid=${job_id}\\b|job_id=${job_id}\\b|action_id=${action_id}' \"\$f\" | tail -120 || echo '(no scoped matches)'; done" 2>/dev/null || true

                if [ "$monitor_alive" = false ]; then
                    _capture_runtime_slurm_logs "monitor-died-after-checkpoint" "$job_id" "$_host" "$sample_dir" "$relevant_job_ids_csv"
                fi
            fi

            poll_slurm_action_status "$action_id" "checkpoint" "$dump_timeout" ||
                {
                    _capture_runtime_slurm_logs "checkpoint-action-timeout" "$job_id" "$_host" "$sample_dir" "$relevant_job_ids_csv"
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

            local old_job_id="$job_id"
            local old_job_name=""
            old_job_name="$(_get_slurm_job_name "$old_job_id")"

            info_log "Cancelling job $job_id before restore..."
            cancel_slurm_job "$job_id"
            sleep 2

            local new_job_id=""
            local detect_timeout=40
            local restore_attempt=1
            local max_restore_attempts=2

            while [ "$restore_attempt" -le "$max_restore_attempts" ]; do
                [ "$restore_attempt" -gt 1 ] &&
                    info_log "Retrying restore for job $old_job_id (attempt $restore_attempt/$max_restore_attempts)..."

                info_log "Restoring job from action $action_id..."
                local restore_output restore_action_id
                if ! restore_output=$(restore_slurm_job "$action_id" "$SLURM_CLUSTER_ID"); then
                    error="Restore failed: $restore_output"
                    break
                fi

                restore_action_id="$restore_output"
                validate_action_id "$restore_action_id" ||
                    {
                        error="Invalid restore action ID: $restore_action_id"
                        break
                    }

                info_log "Restore request returned action_id=$restore_action_id"

                info_log "Waiting for restored job to appear..."
                local elapsed=0

                while [ "$elapsed" -lt "$detect_timeout" ]; do
                    new_job_id=$(_detect_restored_job_id "$old_job_id" "$old_job_name")
                    if [ -n "$new_job_id" ]; then
                        break
                    fi
                    sleep 2
                    elapsed=$((elapsed + 2))
                done

                if [ -n "$new_job_id" ] && [ "$new_job_id" != "$old_job_id" ]; then
                    break
                fi

                if [ "$restore_attempt" -lt "$max_restore_attempts" ]; then
                    info_log "Restore request accepted but no new job ID appeared for cancelled job $old_job_id"
                    restore_attempt=$((restore_attempt + 1))
                    continue
                fi

                break
            done

            if [ -n "$new_job_id" ] && [ "$new_job_id" != "$old_job_id" ]; then
                job_id="$new_job_id"
                tracked_job_ids+=("$job_id")
                relevant_job_ids_csv="$(_slurm_relevant_job_ids_csv "${tracked_job_ids[@]}")"
                info_log "Restored job has new ID: $job_id"
            else
                [ -z "$error" ] && error="No new restored job ID detected for cancelled job $old_job_id"
                break
            fi

            wait_for_slurm_job_state "$job_id" "RUNNING" 60 "$sample_dir" "$relevant_job_ids_csv" ||
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
        _dump_job_failure_info "${job_id:-}" "$sample_dir" "$relevant_job_ids_csv"
        return 1
    fi

    return 0
}
