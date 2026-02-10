#!/bin/bash

#############################
### Performance Helpers   ###
#############################

# Temp directory for spec files
PERF_TEMP_DIR="/tmp"

# Timing helper - returns milliseconds since epoch
now_ms() {
    echo $(($(date +%s%N) / 1000000))
}

# Calculate duration in seconds (with millisecond precision)
duration_seconds() {
    local start_ms="$1"
    local end_ms="$2"
    echo "scale=3; ($end_ms - $start_ms) / 1000" | bc
}

# Convert ISO8601 timestamp to epoch milliseconds
iso_to_ms() {
    local iso_time="$1"
    # Handle both formats: 2024-02-10T18:50:00Z and 2024-02-10T18:50:00.123456789Z
    date -d "$iso_time" +%s%3N 2>/dev/null || echo "0"
}

# Get detailed timing breakdown from pod events/status
# Returns: container_runtime,scheduling_time,image_pull_time,total_time (all in seconds)
get_pod_timing_breakdown() {
    local pod_name="$1"
    local namespace="${2:-$NAMESPACE}"

    local pod_json
    pod_json=$(kubectl get pod "$pod_name" -n "$namespace" -o json 2>/dev/null)

    if [ -z "$pod_json" ]; then
        echo "0,0,0,0"
        return 1
    fi

    # Extract timestamps
    local created_at scheduled_at started_at finished_at

    # Pod creation time
    created_at=$(echo "$pod_json" | jq -r '.metadata.creationTimestamp // empty')

    # Pod scheduled time (from conditions)
    scheduled_at=$(echo "$pod_json" | jq -r '.status.conditions[] | select(.type=="PodScheduled" and .status=="True") | .lastTransitionTime // empty' 2>/dev/null | head -1)

    # Container started time
    started_at=$(echo "$pod_json" | jq -r '.status.containerStatuses[0].state.terminated.startedAt // empty' 2>/dev/null)

    # Container finished time
    finished_at=$(echo "$pod_json" | jq -r '.status.containerStatuses[0].state.terminated.finishedAt // empty' 2>/dev/null)

    # Convert to milliseconds
    local created_ms scheduled_ms started_ms finished_ms
    created_ms=$(iso_to_ms "$created_at")
    scheduled_ms=$(iso_to_ms "$scheduled_at")
    started_ms=$(iso_to_ms "$started_at")
    finished_ms=$(iso_to_ms "$finished_at")

    # Calculate durations
    local scheduling_time_ms=0
    local startup_time_ms=0
    local container_runtime_ms=0
    local total_time_ms=0

    if [ "$scheduled_ms" -gt 0 ] && [ "$created_ms" -gt 0 ]; then
        scheduling_time_ms=$((scheduled_ms - created_ms))
    fi

    if [ "$started_ms" -gt 0 ] && [ "$scheduled_ms" -gt 0 ]; then
        startup_time_ms=$((started_ms - scheduled_ms))
    fi

    if [ "$finished_ms" -gt 0 ] && [ "$started_ms" -gt 0 ]; then
        container_runtime_ms=$((finished_ms - started_ms))
    fi

    if [ "$finished_ms" -gt 0 ] && [ "$created_ms" -gt 0 ]; then
        total_time_ms=$((finished_ms - created_ms))
    fi

    # Convert to seconds
    local scheduling_time startup_time container_runtime total_time
    scheduling_time=$(echo "scale=3; $scheduling_time_ms / 1000" | bc)
    startup_time=$(echo "scale=3; $startup_time_ms / 1000" | bc)
    container_runtime=$(echo "scale=3; $container_runtime_ms / 1000" | bc)
    total_time=$(echo "scale=3; $total_time_ms / 1000" | bc)

    echo "$container_runtime,$scheduling_time,$startup_time,$total_time"
}

# Run a pod spec and measure time to completion
# Returns: container_runtime in seconds, or -1 on failure
# Sets global LAST_POD_NAME for profiling extraction
# Outputs timing info to stderr, container runtime to stdout
# If extract_profile=1, waits for marker file and extracts data before pod completes
measure_pod_completion() {
    local spec="$1"
    local timeout="${2:-600}"
    local namespace="${3:-$NAMESPACE}"
    local extract_profile="${4:-0}"
    local profile_dir="${5:-}"

    local pod_name
    local create_output

    # Apply the spec and capture output to get pod name
    if grep -q "generateName:" "$spec"; then
        create_output=$(kubectl create -f "$spec" -n "$namespace" 2>&1)
    else
        create_output=$(kubectl apply -f "$spec" -n "$namespace" 2>&1)
    fi

    if [ $? -ne 0 ]; then
        echo "Failed to create pod: $create_output" >&2
        echo "-1"
        return 1
    fi

    # Extract pod name from output like "pod/perf-baseline-123456 created"
    pod_name=$(echo "$create_output" | grep -oP 'pod/\K[^ ]+' | head -1)

    # Fallback: try to get from "configured" or other patterns
    if [ -z "$pod_name" ]; then
        pod_name=$(echo "$create_output" | grep -oP '^[^ /]+(?= created| configured)' | head -1)
    fi

    if [ -z "$pod_name" ]; then
        echo "Could not determine pod name from: $create_output" >&2
        echo "-1"
        return 1
    fi

    # Export for profiling extraction
    export LAST_POD_NAME="$pod_name"
    echo "Pod: $pod_name" >&2

    # Wait for pod to complete (Succeeded or Failed)
    local elapsed=0
    local poll_interval=2
    local phase=""
    local profiling_extracted=0
    local container_started_time=""
    local workload_done_time=""

    while [ $elapsed -lt "$timeout" ]; do
        phase=$(kubectl get pod "$pod_name" -n "$namespace" -o jsonpath='{.status.phase}' 2>/dev/null)

        case "$phase" in
            Succeeded|Failed)
                if [ "$phase" == "Failed" ]; then
                    echo "Pod failed" >&2
                    kubectl delete pod "$pod_name" -n "$namespace" --wait=false >/dev/null 2>&1
                    echo "-1"
                    return 1
                fi

                # Get timing breakdown from k8s
                local timing_breakdown
                timing_breakdown=$(get_pod_timing_breakdown "$pod_name" "$namespace")

                local container_runtime scheduling_time startup_time total_time
                IFS=',' read -r container_runtime scheduling_time startup_time total_time <<< "$timing_breakdown"

                echo "  Scheduling: ${scheduling_time}s, Startup: ${startup_time}s, Runtime: ${container_runtime}s" >&2

                # Cleanup
                kubectl delete pod "$pod_name" -n "$namespace" --wait=false >/dev/null 2>&1

                # Return container runtime (the actual workload time)
                echo "$container_runtime"
                return 0
                ;;
            Running)
                # Track when container first enters Running state
                if [ -z "$container_started_time" ]; then
                    container_started_time=$(now_ms)
                fi

                # If profiling is enabled, check for marker file indicating workload is done
                if [ "$extract_profile" -eq 1 ] && [ "$profiling_extracted" -eq 0 ]; then
                    # Check if marker file exists (workload finished, waiting in sleep)
                    if kubectl exec "$pod_name" -n "$namespace" -- test -f /tmp/cedana-profiling-done 2>/dev/null; then
                        # Record workload completion time
                        workload_done_time=$(now_ms)

                        echo "  Workload complete, extracting profiling data..." >&2

                        # Extract profiling data while pod is still running
                        if [ -n "$profile_dir" ]; then
                            extract_profiling_data "$pod_name" "$namespace" "$profile_dir"
                        fi
                        profiling_extracted=1

                        # Calculate runtime from our tracking
                        local tracked_runtime
                        tracked_runtime=$(duration_seconds "$container_started_time" "$workload_done_time")

                        # Get scheduling and startup time from k8s status
                        local timing_breakdown
                        timing_breakdown=$(get_pod_timing_breakdown "$pod_name" "$namespace")
                        local _runtime scheduling_time startup_time _total
                        IFS=',' read -r _runtime scheduling_time startup_time _total <<< "$timing_breakdown"

                        echo "  Profiling extracted, terminating pod..." >&2
                        kubectl delete pod "$pod_name" -n "$namespace" --grace-period=1 >/dev/null 2>&1

                        echo "  Scheduling: ${scheduling_time}s, Startup: ${startup_time}s, Runtime: ${tracked_runtime}s" >&2

                        # Cleanup
                        kubectl delete pod "$pod_name" -n "$namespace" --wait=false >/dev/null 2>&1 || true

                        # Return tracked runtime (actual workload time)
                        echo "$tracked_runtime"
                        return 0
                    fi
                fi

                sleep $poll_interval
                ((elapsed += poll_interval))
                ;;
            *)
                sleep $poll_interval
                ((elapsed += poll_interval))
                ;;
        esac
    done

    echo "Timeout after ${timeout}s (phase: $phase)" >&2
    kubectl delete pod "$pod_name" -n "$namespace" --wait=false >/dev/null 2>&1
    echo "-1"
    return 1
}

# Extract profiling data from pod's /tmp or /tmp/cedana-gpu.container/ directory
# Files: cedana-tsc-profile-<pid>.log, native-tsc-profile-<pid>.log
extract_profiling_data() {
    local pod_name="$1"
    local namespace="${2:-$NAMESPACE}"
    local output_dir="$3"

    mkdir -p "$output_dir"

    echo "  Extracting profiling data to $output_dir" >&2

    local found_files=0

    # Check both locations: /tmp/cedana-gpu.container/ (cedana runtime) and /tmp/ (native runtime)
    for profile_dir in "/tmp/cedana-gpu.container" "/tmp"; do
        # Get list of profiling files
        local profile_files
        profile_files=$(kubectl exec "$pod_name" -n "$namespace" -- ls "$profile_dir" 2>/dev/null | grep -E "(cedana|native)-tsc-profile-.*\.log" || true)

        if [ -n "$profile_files" ]; then
            # Copy each file (suppress tar warnings from kubectl cp)
            for file in $profile_files; do
                kubectl cp "$namespace/$pod_name:$profile_dir/$file" "$output_dir/$file" 2>&1 | grep -v "^tar:" >&2
                if [ -f "$output_dir/$file" ]; then
                    echo "  Copied: $file (from $profile_dir)" >&2
                    found_files=1
                fi
            done
        fi
    done

    if [ "$found_files" -eq 0 ]; then
        echo "  No profiling files found" >&2
    fi
}

# Add ENABLE_PROFILING=1 env var to a pod spec (for cedana runtime)
set_profiling_env() {
    local spec="$1"
    local temp_spec="${PERF_TEMP_DIR}/perf-spec-profile-$(date +%s%N).yaml"

    cp "$spec" "$temp_spec"

    # Use yq for proper YAML manipulation
    if command -v yq &>/dev/null; then
        # Check if env section exists, create if not
        if yq -e '.spec.containers[0].env' "$temp_spec" >/dev/null 2>&1; then
            yq -i '.spec.containers[0].env += [{"name": "ENABLE_PROFILING", "value": "1"}]' "$temp_spec"
        else
            yq -i '.spec.containers[0].env = [{"name": "ENABLE_PROFILING", "value": "1"}]' "$temp_spec"
        fi
    else
        echo "Warning: yq not found, profiling env may not be set correctly" >&2
    fi

    echo "$temp_spec"
}

# Add profiling env vars for native/baseline runtime (needs LD_PRELOAD + volume mount)
set_profiling_env_native() {
    local spec="$1"
    local temp_spec="${PERF_TEMP_DIR}/perf-spec-profile-native-$(date +%s%N).yaml"

    cp "$spec" "$temp_spec"

    # Use yq for proper YAML manipulation
    if command -v yq &>/dev/null; then
        # Check if env section exists, create if not then add vars
        if yq -e '.spec.containers[0].env' "$temp_spec" >/dev/null 2>&1; then
            yq -i '.spec.containers[0].env += [{"name": "ENABLE_PROFILING", "value": "1"}, {"name": "LD_PRELOAD", "value": "/usr/local/lib/libcedana-gpu-tracer.so"}]' "$temp_spec"
        else
            yq -i '.spec.containers[0].env = [{"name": "ENABLE_PROFILING", "value": "1"}, {"name": "LD_PRELOAD", "value": "/usr/local/lib/libcedana-gpu-tracer.so"}]' "$temp_spec"
        fi
        # Check if volumeMounts exists, create if not then add mount
        if yq -e '.spec.containers[0].volumeMounts' "$temp_spec" >/dev/null 2>&1; then
            yq -i '.spec.containers[0].volumeMounts += [{"name": "tracer", "mountPath": "/usr/local/lib/libcedana-gpu-tracer.so", "readOnly": true}]' "$temp_spec"
        else
            yq -i '.spec.containers[0].volumeMounts = [{"name": "tracer", "mountPath": "/usr/local/lib/libcedana-gpu-tracer.so", "readOnly": true}]' "$temp_spec"
        fi
        # Check if volumes exists, create if not then add volume
        if yq -e '.spec.volumes' "$temp_spec" >/dev/null 2>&1; then
            yq -i '.spec.volumes += [{"name": "tracer", "hostPath": {"path": "/usr/local/lib/libcedana-gpu-tracer.so", "type": "File"}}]' "$temp_spec"
        else
            yq -i '.spec.volumes = [{"name": "tracer", "hostPath": {"path": "/usr/local/lib/libcedana-gpu-tracer.so", "type": "File"}}]' "$temp_spec"
        fi
    else
        echo "Warning: yq not found, native profiling will not work" >&2
    fi

    echo "$temp_spec"
}

# Add a sleep at the end of the container command for profiling extraction
# This creates a marker file when main process is done, then sleeps to allow extraction
# Usage: set_profiling_wait <spec_file> <sleep_seconds>
#
# Handles multiple command formats:
# 1. bash -c "script" (multiline) - appends to script
# 2. command: [python3, script, args...] - wraps in bash -c
# 3. args: [-c, script] - wraps in bash -c with command: [bash]
set_profiling_wait() {
    local spec="$1"
    local sleep_seconds="${2:-60}"
    local temp_spec="${PERF_TEMP_DIR}/perf-spec-wait-$(date +%s%N).yaml"

    cp "$spec" "$temp_spec"

    local wait_script="
touch /tmp/cedana-profiling-done
echo 'Main process complete, waiting ${sleep_seconds}s for profiling extraction...'
sleep ${sleep_seconds}"

    # Use yq for proper YAML manipulation
    if ! command -v yq &>/dev/null; then
        echo "Warning: yq not found, profiling wait will not be added" >&2
        echo "$temp_spec"
        return
    fi

    # Detect command structure
    local cmd_type=""
    local first_cmd=""

    # Check if command exists and what type
    if yq -e '.spec.containers[0].command[0]' "$temp_spec" >/dev/null 2>&1; then
        first_cmd=$(yq '.spec.containers[0].command[0]' "$temp_spec")
        if [[ "$first_cmd" == "bash" || "$first_cmd" == "/bin/bash" ]]; then
            # bash -c "script" format - check if command[1] is -c
            local second_cmd
            second_cmd=$(yq '.spec.containers[0].command[1]' "$temp_spec" 2>/dev/null || echo "")
            if [[ "$second_cmd" == "-c" ]]; then
                cmd_type="bash_c_multiline"
            fi
        else
            # command: [binary, args...] format (e.g., python3, go, etc.)
            cmd_type="command_list"
        fi
    elif yq -e '.spec.containers[0].args[0]' "$temp_spec" >/dev/null 2>&1; then
        # args only (container has entrypoint that takes -c)
        local first_arg
        first_arg=$(yq '.spec.containers[0].args[0]' "$temp_spec")
        if [[ "$first_arg" == "-c" ]]; then
            cmd_type="args_c"
        else
            cmd_type="args_list"
        fi
    fi

    case "$cmd_type" in
        bash_c_multiline)
            # Append wait script to the multiline bash script in command[2]
            yq -i ".spec.containers[0].command[2] += \"${wait_script}\"" "$temp_spec"
            ;;
        command_list)
            # Wrap the command list in bash -c and append wait script
            # Get the current command as a shell command string
            local orig_cmd
            orig_cmd=$(yq -o=json '.spec.containers[0].command' "$temp_spec" | jq -r 'map(@sh) | join(" ")')
            local new_script="${orig_cmd}${wait_script}"
            yq -i '.spec.containers[0].command = ["bash", "-c", "'"${new_script}"'"]' "$temp_spec"
            ;;
        args_c)
            # Append to args[1] (the script after -c)
            yq -i ".spec.containers[0].args[1] += \"${wait_script}\"" "$temp_spec"
            ;;
        args_list)
            # Wrap args in bash -c with command: [bash] and append wait
            local orig_args
            orig_args=$(yq -o=json '.spec.containers[0].args' "$temp_spec" | jq -r 'map(@sh) | join(" ")')
            local new_script="${orig_args}${wait_script}"
            yq -i '.spec.containers[0].command = ["bash", "-c", "'"${new_script}"'"]' "$temp_spec"
            yq -i 'del(.spec.containers[0].args)' "$temp_spec"
            ;;
        *)
            echo "Warning: Could not determine command structure for profiling wait" >&2
            ;;
    esac

    echo "$temp_spec"
}

# Pretty-print profiling data from a log file
print_profiling_summary() {
    local profile_file="$1"
    local label="${2:-}"

    if [ ! -f "$profile_file" ]; then
        return 1
    fi

    local filename
    filename=$(basename "$profile_file")

    echo ""
    if [ -n "$label" ]; then
        echo "=== Profiling: $label ($filename) ==="
    else
        echo "=== Profiling: $filename ==="
    fi

    # The profiling output is already formatted, just cat it
    # But we can extract key metrics if needed
    cat "$profile_file"
}

# Modify a YAML spec to set or remove runtimeClassName
# Usage: set_runtime_class <spec_file> <runtime_class|"">
# If runtime_class is empty, removes the runtimeClassName field
set_runtime_class() {
    local spec="$1"
    local runtime_class="$2"
    local temp_spec="${PERF_TEMP_DIR}/perf-spec-$(date +%s%N).yaml"

    cp "$spec" "$temp_spec"

    if [ -n "$runtime_class" ]; then
        # Add or replace runtimeClassName
        if grep -q "runtimeClassName:" "$temp_spec"; then
            sed -i "s/runtimeClassName:.*/runtimeClassName: $runtime_class/" "$temp_spec"
        else
            # Add after "spec:" line (pod spec level)
            sed -i '/^spec:/a\  runtimeClassName: '"$runtime_class" "$temp_spec"
        fi
    else
        # Remove runtimeClassName line
        sed -i '/runtimeClassName:/d' "$temp_spec"
    fi

    echo "$temp_spec"
}

# Modify epochs in a pytorch training spec
# Looks for --epochs N pattern and replaces it
set_epochs() {
    local spec="$1"
    local epochs="$2"
    local temp_spec="${PERF_TEMP_DIR}/perf-spec-epochs-$(date +%s%N).yaml"

    cp "$spec" "$temp_spec"

    # Replace --epochs N with new value
    sed -i "s/--epochs [0-9]\+/--epochs $epochs/g" "$temp_spec"

    echo "$temp_spec"
}

# Modify pod name to ensure uniqueness
set_unique_name() {
    local spec="$1"
    local prefix="${2:-perf}"
    local temp_spec="${PERF_TEMP_DIR}/perf-spec-name-$(date +%s%N).yaml"

    cp "$spec" "$temp_spec"

    local unique_id="${prefix}-$(date +%s%N)"

    # Handle both name: and generateName:
    if grep -q "generateName:" "$temp_spec"; then
        sed -i "s/generateName:.*/generateName: ${unique_id}-/" "$temp_spec"
    elif grep -q "^  name:" "$temp_spec"; then
        sed -i "s/^  name:.*/  name: ${unique_id}/" "$temp_spec"
    fi

    echo "$temp_spec"
}

# Calculate statistics from an array of numbers
# Usage: calc_stats "${times[@]}"
# Outputs: mean,stddev,min,max
calc_stats() {
    local -a nums=("$@")
    local n=${#nums[@]}

    if [ $n -eq 0 ]; then
        echo "0,0,0,0"
        return
    fi

    # Calculate using awk for precision
    printf '%s\n' "${nums[@]}" | awk '
    {
        sum += $1
        sumsq += ($1 * $1)
        if (NR == 1 || $1 < min) min = $1
        if (NR == 1 || $1 > max) max = $1
        n++
    }
    END {
        mean = sum / n
        if (n > 1) {
            variance = (sumsq - (sum * sum) / n) / (n - 1)
            stddev = sqrt(variance)
        } else {
            stddev = 0
        }
        printf "%.3f,%.3f,%.3f,%.3f\n", mean, stddev, min, max
    }'
}

# Print a formatted results table
print_results() {
    local workload="$1"
    local samples="$2"
    local -n baseline_times=$3
    local -n cedana_times=$4

    local baseline_stats
    local cedana_stats
    baseline_stats=$(calc_stats "${baseline_times[@]}")
    cedana_stats=$(calc_stats "${cedana_times[@]}")

    local baseline_mean baseline_stddev baseline_min baseline_max
    local cedana_mean cedana_stddev cedana_min cedana_max

    IFS=',' read -r baseline_mean baseline_stddev baseline_min baseline_max <<< "$baseline_stats"
    IFS=',' read -r cedana_mean cedana_stddev cedana_min cedana_max <<< "$cedana_stats"

    # Calculate overhead
    local overhead
    if [ "$(echo "$baseline_mean > 0" | bc)" -eq 1 ]; then
        overhead=$(echo "scale=2; (($cedana_mean - $baseline_mean) / $baseline_mean) * 100" | bc)
    else
        overhead="N/A"
    fi

    echo ""
    echo "========================================"
    echo "Performance Results (Container Runtime)"
    echo "========================================"
    echo "Workload: $workload"
    echo "Samples:  $samples"
    echo ""
    echo "Baseline (nvidia runtime):"
    echo "  Times:  ${baseline_times[*]}"
    echo "  Mean:   ${baseline_mean}s"
    echo "  StdDev: ${baseline_stddev}s"
    echo "  Min:    ${baseline_min}s"
    echo "  Max:    ${baseline_max}s"
    echo ""
    echo "Cedana (cedana runtime):"
    echo "  Times:  ${cedana_times[*]}"
    echo "  Mean:   ${cedana_mean}s"
    echo "  StdDev: ${cedana_stddev}s"
    echo "  Min:    ${cedana_min}s"
    echo "  Max:    ${cedana_max}s"
    echo ""
    echo "Overhead: ${overhead}%"
    echo "========================================"
}

# Output results as JSON
output_json() {
    local workload="$1"
    local samples="$2"
    local -n baseline_times=$3
    local -n cedana_times=$4
    local output_file="${5:-}"

    local baseline_stats cedana_stats
    baseline_stats=$(calc_stats "${baseline_times[@]}")
    cedana_stats=$(calc_stats "${cedana_times[@]}")

    local baseline_mean baseline_stddev baseline_min baseline_max
    local cedana_mean cedana_stddev cedana_min cedana_max

    IFS=',' read -r baseline_mean baseline_stddev baseline_min baseline_max <<< "$baseline_stats"
    IFS=',' read -r cedana_mean cedana_stddev cedana_min cedana_max <<< "$cedana_stats"

    local overhead
    if [ "$(echo "$baseline_mean > 0" | bc)" -eq 1 ]; then
        overhead=$(echo "scale=4; (($cedana_mean - $baseline_mean) / $baseline_mean) * 100" | bc)
    else
        overhead="null"
    fi

    # Build JSON arrays for times
    local baseline_json cedana_json
    baseline_json=$(printf '%s\n' "${baseline_times[@]}" | jq -s '.')
    cedana_json=$(printf '%s\n' "${cedana_times[@]}" | jq -s '.')

    local json
    json=$(jq -n \
        --arg workload "$workload" \
        --argjson samples "$samples" \
        --argjson baseline_times "$baseline_json" \
        --argjson baseline_mean "$baseline_mean" \
        --argjson baseline_stddev "$baseline_stddev" \
        --argjson baseline_min "$baseline_min" \
        --argjson baseline_max "$baseline_max" \
        --argjson cedana_times "$cedana_json" \
        --argjson cedana_mean "$cedana_mean" \
        --argjson cedana_stddev "$cedana_stddev" \
        --argjson cedana_min "$cedana_min" \
        --argjson cedana_max "$cedana_max" \
        --argjson overhead "$overhead" \
        '{
            timestamp: now | strftime("%Y-%m-%dT%H:%M:%SZ"),
            workload: $workload,
            samples: $samples,
            timing: "container_runtime",
            baseline: {
                times_seconds: $baseline_times,
                mean: $baseline_mean,
                stddev: $baseline_stddev,
                min: $baseline_min,
                max: $baseline_max
            },
            cedana: {
                times_seconds: $cedana_times,
                mean: $cedana_mean,
                stddev: $cedana_stddev,
                min: $cedana_min,
                max: $cedana_max
            },
            overhead_percent: $overhead
        }')

    if [ -n "$output_file" ]; then
        echo "$json" > "$output_file"
        echo "Results written to: $output_file" >&2
    else
        echo "$json"
    fi
}
