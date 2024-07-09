#!/bin/bash

# args
remote=false
pattern=""
# queried
checkpoint_sandbox=""
restore_sandbox=""
export count=1

setup() {
  ./reset.sh
  otelcol-contrib --config test/benchmarks/local-otelcol-config.yaml &
  ./build-start-daemon.sh --systemctl
}

usage() {
  echo "Usage: $0 [--remote] [--pattern <base|9to5|8h_varied|std_range|any_range|on_off>]"
  exit 1
}

is_valid_pattern() {
  valid_patterns=("base" "9to5" "8h_varied" "std_range" "any_range" "on_off")
  local value=$1
  for valid_pattern in "${valid_patterns[@]}"; do
    if [[ "$valid_pattern" == "$value" ]]; then
      return 0
    fi
  done
  return 1
}

parse_args () {
  while [[ "$#" -gt 0 ]]; do
    case $1 in
      --remote)
        remote=true
        ;;
      --pattern)
        if [[ -n $2 && ! $2 == --* ]]; then
          if is_valid_pattern "$2"; then
            pattern=$2
          else
            echo "Error: Invalid pattern."
            usage
          fi
          shift
        else
          echo "Error: --pattern requires a non-empty argument."
          usage
        fi
        ;;
      *)
        echo "Unknown argument $1"
        usage
        ;;
    esac
    shift
  done
  if [[ -z $pattern ]]; then
    echo "Error: --pattern is required."
    usage
  fi
}

print_time() {
  local hours=$1
  local minutes=$2
  local seconds=$3
  local total_mins=$4
  echo "$hours:$minutes:$seconds => $total_mins minutes"
}

print_utilization() {
  echo "{'time': $1, 'utilization': $2, 'suspend': $3, 'migrate': $4, 'restore': $5}"
}

wait_until_next_minute() {
  current_second=$(date +"%S")
  remaining_seconds=$((60 - 10#$current_second))
  sleep $remaining_seconds
}

set_restore_sandbox() {
  restore_container=$1
  echo "set_restore_sandbox(): checkpoint_sandbox=$checkpoint_sandbox, restore_container=$restore_container"
  restore_sandbox=$(curl -s -X GET -H 'Content-Type: application/json' -d '{
    "root": "/run/containerd/runc/k8s.io" }' http://localhost:1324/list/default | \
    jq -r '.[] | select( any(.[]; .SandboxName == "'$checkpoint_sandbox'")) as $node |
    ($node | map(select(.ContainerName == "'$restore_container'")) | .[0].SandboxName)')
  if [ $? -ne 0 ]; then
    echo "Error: no containers found - nodes still starting up?"
    exit 1
  fi
  if [[ -z "$checkpoint_sandbox" || -z "$restore_sandbox" ]]; then # does not detect null, only empty
    echo "Error: no sandbox found - check EKS connection"
    exit 1
  fi
  echo "Found checkpoint_sandbox = $checkpoint_sandbox, restore_sandbox = $restore_sandbox"
}

checkpoint() {
  local checkpoint_path=$1
  set_restore_sandbox "base-container-"$count
  checkpoint_container="base-container-"$(($count-1))
  echo "Checkpointing into $checkpoint_path from $checkpoint_sandbox (container: $checkpoint_container)"
  curl -v -X POST -H "Content-Type: application/json" -d '{
    "checkpoint_data": {
      "container_name": "'$checkpoint_container'",
      "sandbox_name": "'$checkpoint_sandbox'",
      "namespace": "default",
      "checkpoint_path": "'$checkpoint_path'",
      "root": "/run/containerd/runc/k8s.io"
    },
    "leave_running": true
  }' http://localhost:1324/checkpoint; echo
}

restore() {
  local checkpoint_path=$1
  envsubst < test/benchmarks/base-continuous.yaml | kubectl apply -f -
  echo "Restoring from $checkpoint_path into $restore_sandbox"
  status=0
  curl -o $status -v -X POST -H "Content-Type: application/json" -d '{
    "checkpoint_data": {
      "container_name": "'$restore_container'",
      "sandbox_name": "'$restore_sandbox'",
      "namespace": "default",
      "checkpoint_path": "'$checkpoint_path'",
      "root": "/run/containerd/runc/k8s.io"
    }
  }' http://localhost:1324/restore; echo
  echo "status $status"
}

base() {
  echo "Running base simulation"
  cedana exec -w $PWD "code-server --bind-addr localhost:1234"
  while true; do
    hours=$(date +"%H")
    minutes=$(date +"%M")
    seconds=$(date +"%S")
    total_mins=$((10#$hours * 60 + 10#$minutes))
    print_time "$hours" "$minutes" "$seconds" "$total_mins"
    utilization=true
    suspend=false
    migrate=false
    restore=false
    print_utilization $total_mins $utilization $suspend $migrate $restore
    wait_until_next_minute
  done
}

9to5() {
  echo "Running 9to5 simulation"
  cedana exec -w $PWD "code-server --bind-addr localhost:1234"
  checkpoint_path=""
  set_sandbox_names
  while true; do
    hours=$(date +"%H")
    minutes=$(date +"%M")
    seconds=$(date +"%S")
    t=$((10#$hours * 60 + 10#$minutes)) # time in minutes
    print_time "$hours" "$minutes" "$seconds" "$t"
    start=540
    end=1020
    if [[ $t -ge $start && $t -lt $end ]]; then u=true; else u=false; fi
    if [[ $t -eq $end ]]; then
      s=true;
      checkpoint_path=/tmp/ckpt-$hours-$minutes-$seconds
      checkpoint $checkpoint_path
    else s=false; fi
    if [[ $t -eq $start ]]; then m=true; else m=false; fi
    if [[ $t -eq $(($start + 1)) ]]; then
      r=true;
      restore $checkpoint_path
    else r=false; fi
    print_utilization $t $u $s $m $r
    wait_until_next_minute
  done
}

8h_varied() {
  echo "Running 8h_varied simulation"
  cedana exec -w $PWD "code-server --bind-addr localhost:1234"
  while true; do
    hours=$(date +"%H")
    minutes=$(date +"%M")
    seconds=$(date +"%S")
    t=$((10#$hours * 60 + 10#$minutes)) # time in minutes
    print_time "$hours" "$minutes" "$seconds" "$t"
    start=$((RANDOM % 361 + 360)) # between 360 (6am) and 720 (12pm)
    end=$(($start + 8 * 60)) # 8h after start
    if [[ $t -ge $start && $t -lt $end ]]; then u=true; else u=false; fi
    if [[ $t -eq $end ]]; then s=true; else s=false; fi
    if [[ $t -eq $start ]]; then m=true; else m=false; fi
    if [[ $t -eq $(($start + 1)) ]]; then r=true; else r=false; fi
    print_utilization $t $u $s $m $r
    wait_until_next_minute
  done
}

std_range() {
  echo "Running std_range simulation"
  cedana exec -w $PWD "code-server --bind-addr localhost:1234"
  while true; do
    hours=$(date +"%H")
    minutes=$(date +"%M")
    seconds=$(date +"%S")
    t=$((10#$hours * 60 + 10#$minutes)) # time in minutes
    print_time "$hours" "$minutes" "$seconds" "$t"
    start=$((RANDOM % 361 + 360)) # between 360 (6am) and 720 (12pm)
    end=$((RANDOM % 361 + 840)) # between 840 (2pm) and 1200 (8pm)
    if [[ $t -ge $start && $t -lt $end ]]; then u=true; else u=false; fi
    if [[ $t -eq $end ]]; then s=true; else s=false; fi
    if [[ $t -eq $start ]]; then m=true; else m=false; fi
    if [[ $t -eq $(($start + 1)) ]]; then r=true; else r=false; fi
    print_utilization $t $u $s $m $r
    wait_until_next_minute
  done
}

on_off () {
  echo "Running on_off simulation"
  echo $PWD
  kubectl apply -f $PWD/test/benchmarks/continuous.yaml
  export count=1
  envsubst < test/benchmarks/base-continuous.yaml | kubectl apply -f -
  sleep 30
  checkpoint_sandbox=$(curl -s -X GET -H 'Content-Type: application/json' -d '{
    "root": "/run/containerd/runc/k8s.io" }' http://localhost:1324/list/default | \
    jq -r '.[][] | select(.ContainerName == "base-container-0") | .SandboxName')
  if [ $? -ne 0 ]; then
    echo "Error: no containers found - nodes still starting up?"
    exit 1
  fi
  checkpoint_path=/tmp/ckpt-init
  echo "Initial checkpoint at $checkpoint_path"
  checkpoint $checkpoint_path
  stime=$((10#$(date +"%H") * 60 + 10#$(date +"%M")))
  while true; do
    hours=$(date +"%H")
    minutes=$(date +"%M")
    seconds=$(date +"%S")
    t=$((10#$hours * 60 + 10#$minutes)) # time in minutes
    print_time "$hours" "$minutes" "$seconds" "$t"
    if [[ $(($(($t-$stime)) % 3)) -eq 0 ]]; then m=true; else m=false; fi
    if [[ $(($(($t-$stime)) % 3)) -eq 1 ]]; then
      r=true
      export count=$(($count+1))
      restore $checkpoint_path $count
      checkpoint_sandbox=$restore_sandbox
    else r=false; fi
    if [[ $(($(($t-$stime)) % 3)) -eq 2 ]]; then
      s=true
      checkpoint_path=/tmp/ckpt-$hours-$minutes-$seconds
      checkpoint $checkpoint_path
    else s=false; fi
    u=true

    #if [[ $(($(($t-$stime)) % 10)) -lt 7 ]]; then u=true; else u=false; fi
    #if [[ $(($(($t-$stime)) % 10)) -eq 6 ]]; then
    #  s=true
    #  checkpoint_path=/tmp/ckpt-$hours-$minutes-$seconds
    #  checkpoint $checkpoint_path
    #else s=false; fi
    #if [[ $(($(($t-$stime)) % 10)) -eq 0 ]]; then m=true; else m=false; fi
    #if [[ $(($(($t-$stime)) % 10)) -eq 1 ]]; then
    #  r=true
    #  export count=$(($count+1))
    #  restore $checkpoint_path $count
    #  checkpoint_sandbox=$restore_sandbox
    #else r=false; fi
#   # if [[ $(($(($t-$stime)) % 10)) -eq 2 ]]; then
#      node_name=$(kubectl get pod -l app=continuous -o jsonpath="{.items[0].spec.nodeName}") # initial job has app=continuous
#      echo "node name - $node_name"
#      pod_name=$(kubectl get pods --all-namespaces --field-selector spec.nodeName=$node_name -o json | jq -r '.items[] | select(.metadata.name | startswith("binary-daemonset")) | .metadata.name')
#      echo "pod name - $pod_name"
#      echo "ls /host/tmp/ (pre-delete)"
#      kubectl exec -it $pod_name -n cedanacontroller-system -- ls /host/tmp
#      echo "rm -Rf /host$checkpoint_path"
#      kubectl exec -it $pod_name -n cedanacontroller-system -- rm -Rf /host$checkpoint_path /host$checkpoint_path.tar
#      echo "ls /host/tmp/ (post-delete)"
#      kubectl exec -it $pod_name -n cedanacontroller-system -- ls /host/tmp
#    fi
    print_utilization $t $u $s $m $r
    wait_until_next_minute
  done
}

#setup
parse_args "$@"
if [[ "$pattern" == "base" ]]; then
  base
elif [[ "$pattern" == "9to5" ]]; then
  9to5
elif [[ "$pattern" == "8h_varied" ]]; then
  8h_varied
elif [[ "$pattern" == "std_range" ]]; then
  std_range
elif [[ "$pattern" == "any_range" ]]; then
  any_range
elif [[ "$pattern" == "on_off" ]]; then
  on_off
else
  echo "other valid_pattern"
fi


