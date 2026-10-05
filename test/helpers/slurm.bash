#!/usr/bin/env bash

################################
# SLURM Job Management & C/R  #
################################

# Source setup helpers (shared vars + functions)
source "$(dirname "${BASH_SOURCE[0]}")/slurm_setup.bash"

# Where slurm_submit_script submits from, on the submission host. Every node has
# its own clone of cedana-samples here.
SLURM_SCRIPT_WORKDIR=/data/cedana-samples

# Runs sbatch from <workdir> on the submission host and prints the job ID.
# Submits <file> (a path there) if given, otherwise the script on stdin.
_slurm_sbatch() {
    local workdir="$1"
    local file="${2:-}"
    local cedana_enable="${CEDANA_ENABLE:-1}"
    local cedana_bin="${CEDANA_BIN:-/usr/local/bin/cedana}"
    local exec_opts=() output job_id

    [ -n "$file" ] || exec_opts+=(-i)

    if ! output=$(slurm_submit_exec "${exec_opts[@]}" bash -c 'cd "$1" && shift && exec sbatch "$@"' _ \
        "$workdir" --parsable --overcommit \
        --export=ALL,CEDANA_ENABLE="${cedana_enable}",CEDANA_BIN="${cedana_bin}" \
        --cpus-per-task=1 --mem=0 ${file:+"$file"} 2>&1); then
        error_log "sbatch failed: $output"
        return 1
    fi

    job_id=$(echo "$output" | tail -1 | cut -d';' -f1 | tr -d '[:space:]')
    echo "$job_id"
}

# Submits a cedana-samples sbatch file, from its directory in the nodes' clone.
slurm_submit_job() {
    local sbatch_file="$1"
    local container_dir container_file job_id

    container_dir="$(_slurm_sample_container_dir "$sbatch_file")"
    container_file="$(basename "$sbatch_file")"
    info_log "Submitting from $(slurm_submission_container): cd $container_dir && sbatch $container_file"

    job_id="$(_slurm_sbatch "$container_dir" "$container_file")" || return 1
    info_log "Submitted $container_file -> job $job_id"
    echo "$job_id"
}

# Submits a batch script that lives on the runner rather than in cedana-samples,
# so a test controls exactly what its job does. The script goes to sbatch on
# stdin from SLURM_SCRIPT_WORKDIR, so it can still run the samples' workloads by
# relative path (e.g. cpu_smr/counting.sh), and its output lands where the
# failure diagnostics look.
slurm_submit_script() {
    local script_file="$1"
    local job_id

    [ -f "$script_file" ] || {
        error_log "sbatch script not found: $script_file"
        return 1
    }

    info_log "Submitting $script_file from $(slurm_submission_container): cd $SLURM_SCRIPT_WORKDIR && sbatch <script>"

    job_id="$(_slurm_sbatch "$SLURM_SCRIPT_WORKDIR" <"$script_file")" || return 1
    info_log "Submitted $(basename "$script_file") -> job $job_id"
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
            docker exec "$container" cat "$src" >"$dst_dir/$base" 2>&1 || true
    fi
}

# Print the cedana logs in the home directories of the node's regular users.
# cedana logs to /var/log only as root; as any other user it logs to
# ~/.cedana/logs (pkg/logging.LogFile). In unprivileged mode that is where the
# monitor, and the dump or restore it runs, leave everything they log. Homes
# come from the same passwd entries cedana looks them up in.
#
# The users can name these files anything, so the paths are NUL-separated and
# must only ever be passed as arguments, never put into shell code.
_slurm_user_log_files() {
    local container="$1"

    docker exec "$container" sh -c '
        getent passwd | awk -F: "\$3 >= 1000 && \$3 < 65534 { print \$6 }" | sort -u |
            while read -r home; do
                for f in "$home"/.cedana/logs/*.log; do
                    [ -f "$f" ] && printf "%s\0" "$f"
                done
            done
    ' 2>/dev/null || true
}

# Print the cedana-slurm logs in /var/log and the users' logs, NUL-separated.
_slurm_cedana_log_files() {
    printf '%s\0' /var/log/cedana-slurm.log /var/log/cedana-slurm-monitor.log
    _slurm_user_log_files "$1"
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

        # By user, since each user has its own cedana-slurm.log. Not under a
        # .cedana directory, which the artifact upload skips as hidden.
        local user_log
        while IFS= read -r -d "" user_log; do
            [ -n "$user_log" ] || continue
            _persist_container_log_file "$c" "$user_log" \
                "$cdir/user-logs/$(basename "${user_log%/.cedana/logs/*}")"
        done < <(_slurm_user_log_files "$c")

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
                            for f in "$sample_dir/slurm-$id.$suffix" "$sample_dir"/*-"$id.$suffix"; do
                                [ -f "$f" ] && printf "%s\n" "$f"
                            done
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
    local job_show out_file err_file found

    _capture_runtime_slurm_logs "failure-dump" "$job_id" "unknown" "$sample_dir" "$relevant_job_ids_csv"

    echo "=== sacct (last 10 jobs) ==="
    slurm_exec sacct --noheader -a \
        --format=JobID,JobName,State,ExitCode,DerivedExitCode,Reason,NodeList,Submit,Start,End \
        -P 2>/dev/null | tail -10 || true

    if [ -n "$job_id" ]; then
        echo "=== scontrol show job $job_id ==="
        slurm_exec scontrol show job "$job_id" 2>/dev/null || true

        echo "=== job output files ==="
        # Ask SLURM where it put them; the paths come from the sbatch
        # --output/--error directives and are not guessable.
        job_show="$(slurm_exec scontrol show job "$job_id" 2>/dev/null || true)"
        out_file="$(printf '%s' "$job_show" | grep -oE 'StdOut=[^[:space:]]+' | head -1 | cut -d= -f2-)"
        err_file="$(printf '%s' "$job_show" | grep -oE 'StdErr=[^[:space:]]+' | head -1 | cut -d= -f2-)"

        found=0
        for c in "$SLURM_CONTROLLER_CONTAINER" $(_slurm_compute_containers); do
            for f in "$out_file" "$err_file"; do
                [ -n "$f" ] || continue
                docker exec "$c" test -f "$f" 2>/dev/null || continue
                echo "--- $c:$f ---"
                docker exec "$c" tail -50 "$f" 2>/dev/null || true
                found=1
            done
        done
        [ "$found" -eq 1 ] ||
            echo "(no output for job $job_id; StdOut=${out_file:-?} StdErr=${err_file:-?})"
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

        local user_log
        while IFS= read -r -d "" user_log; do
            [ -n "$user_log" ] || continue
            echo "--- $c $user_log (last 120 lines) ---"
            docker exec "$c" tail -120 "$user_log" 2>/dev/null || echo "(unreadable)"
        done < <(_slurm_user_log_files "$c")
    done

    echo "=== cedana-slurm log on controller (last 50 lines) ==="
    docker exec "$SLURM_CONTROLLER_CONTAINER" \
        tail -50 /var/log/cedana-slurm.log 2>/dev/null || true

    # The 50-line tail is mostly sync debug spam, so a job-sync failure minutes
    # earlier scrolls off; that error is what explains a job never registering.
    # Capture first: grep's "no matches" status is lost through the pipe, so
    # `|| echo` on the pipeline would never fire and the section would be blank.
    echo "=== job sync errors on controller ==="
    local sync_errors
    sync_errors="$(docker exec "$SLURM_CONTROLLER_CONTAINER" \
        grep -aiE "Failed to get SLURM jobs|Failed to send sync request|non-OK status for job sync|Failed to marshal sync" \
        /var/log/cedana-slurm.log 2>/dev/null | tail -20)"
    echo "${sync_errors:-(none)}"

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

# Runs an action sequence (e.g. SUBMIT_DUMP_RESTORE) against a sample job.
#
# Set SLURM_JOB_CHECK to the name of a function to run extra checks on the
# job: it is called as `<fn> pre-dump <job_id>` just before each dump and as
# `<fn> post-restore <job_id>` once a restored job is running. A non-zero
# return fails the sequence.
#
# Set SLURM_JOB_SUBMIT=slurm_submit_script to submit a script from the runner
# instead of a cedana-samples file.
test_slurm_job() {
    local action_sequence="$1"
    local sbatch_file="$2"
    local dump_wait_time="${3:-10}"
    local dump_timeout="${4:-120}"
    local submit_fn="${SLURM_JOB_SUBMIT:-slurm_submit_job}"
    local sample_dir=""
    local relevant_job_ids_csv=""
    local tracked_job_ids=()

    IFS='_' read -ra actions <<<"$action_sequence"
    if [ "$submit_fn" = "slurm_submit_script" ]; then
        sample_dir="$SLURM_SCRIPT_WORKDIR"
    else
        sample_dir="$(_slurm_sample_container_dir "$sbatch_file")"
    fi

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
            job_id=$("$submit_fn" "$sbatch_file") ||
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

            if [ -n "${SLURM_JOB_CHECK:-}" ]; then
                "$SLURM_JOB_CHECK" pre-dump "$job_id" ||
                    {
                        error="Pre-dump check failed for job $job_id"
                        break
                    }
            fi

            local _host
            local -a _cedana_logs=()
            _host="$(_get_batch_host "$job_id")"
            if [ -n "$_host" ]; then
                mapfile -d '' -t _cedana_logs < <(_slurm_cedana_log_files "$_host")
                info_log "[DEBUG] SPANK monitor check on $_host for job $job_id:"
                docker exec "$_host" bash -c "ps -eo pid,ppid,stat,cmd | grep -E '[c]edana-slurm monitor'" 2>/dev/null || info_log "[DEBUG] No monitor process found"
                info_log "[DEBUG] slurmd PATH:"
                docker exec "$_host" bash -c "cat /proc/\$(pgrep -x slurmd | head -1)/environ 2>/dev/null | tr '\0' '\n' | grep ^PATH" 2>/dev/null || info_log "[DEBUG] Could not read slurmd environ"
                info_log "[DEBUG] SPANK log entries:"
                # The log paths go in as arguments: users name their own logs
                docker exec "$_host" bash -c '
                    for f in "$@"; do
                        [ -f "$f" ] || continue
                        echo "--- $f ---"
                        grep -i "spank\|monitor\|checkpoint request\|checkpoint consumer\|failed to get event stream\|failed to setup checkpoint request consumer\|failed to connect to rabbitmq\|checkpoint failed for job ID\|publishing checkpoint info" "$f" | tail -15
                    done
                ' _ "${_cedana_logs[@]}" 2>/dev/null || info_log "[DEBUG] No SPANK/monitor entries in logs"
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
                # Again, as a user's log only exists once something has logged there as them
                mapfile -d '' -t _cedana_logs < <(_slurm_cedana_log_files "$_host")
                docker exec "$_host" bash -c '
                    pattern="$1"
                    shift
                    for f in "$@"; do
                        [ -f "$f" ] || continue
                        echo "--- $f (job/action scoped) ---"
                        grep -E "$pattern" "$f" | tail -120 || echo "(no scoped matches)"
                    done
                ' _ "jobid=${job_id}\\b|job_id=${job_id}\\b|action_id=${action_id}" "${_cedana_logs[@]}" 2>/dev/null || true

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

            for c in $(_slurm_compute_containers); do
                docker exec "$c" bash -c '
                    for mp in /usr/local/bin /usr/local/lib /usr/local/src /usr/lib/slurm; do
                        mountpoint -q "$mp" 2>/dev/null && umount -l "$mp" 2>/dev/null
                    done
                    mount -a 2>/dev/null
                ' 2>/dev/null || true
            done

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

            if [ -n "${SLURM_JOB_CHECK:-}" ]; then
                "$SLURM_JOB_CHECK" post-restore "$job_id" ||
                    {
                        error="Post-restore check failed for job $job_id"
                        break
                    }
            fi
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
        if [ "$submit_fn" = "slurm_submit_script" ]; then
            info_log "Submitted script ($sbatch_file):"
            cat "$sbatch_file" >&"${OUTPUT_FD}" || true
        fi
        slurm_exec squeue 2>/dev/null || true
        slurm_exec sinfo 2>/dev/null || true
        _dump_job_failure_info "${job_id:-}" "$sample_dir" "$relevant_job_ids_csv"
        return 1
    fi

    return 0
}
