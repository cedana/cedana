#!/usr/bin/env bats

# bats file_tags=slurm,ansible

load ../helpers/utils
load ../helpers/slurm

CONTROLLER="slurm-controller"
COMPUTE_01="slurm-compute-01"
LOGIN_01="slurm-login-01"

##############################
# Container layout
##############################

@test "Ansible: controller, compute, login containers are running" {
    for c in "$CONTROLLER" "$COMPUTE_01" "$LOGIN_01"; do
        run docker inspect -f '{{.State.Running}}' "$c"
        [ "$status" -eq 0 ]
        [ "$output" = "true" ]
    done
}

@test "Ansible: each container's hostname matches its inventory name" {
    for c in "$CONTROLLER" "$COMPUTE_01" "$LOGIN_01"; do
        run docker exec "$c" hostname
        [ "$status" -eq 0 ]
        [ "$output" = "$c" ]
    done
}

##############################
# Service distribution
##############################

@test "Ansible: controller runs slurmctld" {
    run docker exec "$CONTROLLER" pgrep -x slurmctld
    [ "$status" -eq 0 ]
}

@test "Ansible: compute runs slurmd, no slurmctld" {
    run docker exec "$COMPUTE_01" pgrep -x slurmd
    [ "$status" -eq 0 ]
    run docker exec "$COMPUTE_01" pgrep -x slurmctld
    [ "$status" -ne 0 ]
}

@test "Ansible: login runs neither slurmd nor slurmctld" {
    run docker exec "$LOGIN_01" pgrep -x slurmd
    [ "$status" -ne 0 ]
    run docker exec "$LOGIN_01" pgrep -x slurmctld
    [ "$status" -ne 0 ]
}

##############################
# Munge auth
##############################

@test "Ansible: munge running on every node" {
    for c in "$CONTROLLER" "$COMPUTE_01" "$LOGIN_01"; do
        run docker exec "$c" pgrep -x munged
        [ "$status" -eq 0 ]
    done
}

@test "Ansible: munge key checksum matches across nodes" {
    local controller_md5
    controller_md5=$(docker exec "$CONTROLLER" md5sum /etc/munge/munge.key | awk '{print $1}')
    [ -n "$controller_md5" ]
    for c in "$COMPUTE_01" "$LOGIN_01"; do
        local m
        m=$(docker exec "$c" md5sum /etc/munge/munge.key | awk '{print $1}')
        [ "$m" = "$controller_md5" ]
    done
}

@test "Ansible: munge round-trip auth from login to controller" {
    run docker exec "$LOGIN_01" bash -c 'munge -n | unmunge'
    [ "$status" -eq 0 ]
    [[ "$output" == *"STATUS:"*"Success"* ]]
}

##############################
# Configuration propagation
##############################

@test "Ansible: slurm.conf present on every node" {
    for c in "$CONTROLLER" "$COMPUTE_01" "$LOGIN_01"; do
        run docker exec "$c" test -f /etc/slurm/slurm.conf
        [ "$status" -eq 0 ]
    done
}

@test "Ansible: cgroup.conf present on every node" {
    for c in "$CONTROLLER" "$COMPUTE_01" "$LOGIN_01"; do
        run docker exec "$c" test -f /etc/slurm/cgroup.conf
        [ "$status" -eq 0 ]
    done
}

@test "Ansible: SlurmctldHost in slurm.conf points to the controller" {
    run docker exec "$LOGIN_01" grep -E '^SlurmctldHost=' /etc/slurm/slurm.conf
    [ "$status" -eq 0 ]
    [[ "$output" == *"$CONTROLLER"* ]]
}

##############################
# NFS shared install
##############################

@test "Ansible: NFS shares mounted on compute" {
    run docker exec "$COMPUTE_01" findmnt -t nfs4 --noheadings -o TARGET
    [ "$status" -eq 0 ]
    [[ "$output" == *"/usr/local/bin"* ]]
    [[ "$output" == *"/usr/local/lib"* ]]
}

@test "Ansible: NFS shares mounted on login" {
    run docker exec "$LOGIN_01" findmnt -t nfs4 --noheadings -o TARGET
    [ "$status" -eq 0 ]
    [[ "$output" == *"/usr/local/bin"* ]]
    [[ "$output" == *"/usr/local/lib"* ]]
}

@test "Ansible: NFS root_squash blocks compute writes to /usr/local/bin" {
    run docker exec "$COMPUTE_01" bash -c 'touch /usr/local/bin/.write-test 2>&1'
    [ "$status" -ne 0 ]
}

##############################
# Cluster operation
##############################

@test "Ansible: sinfo works from controller, compute, and login" {
    for c in "$CONTROLLER" "$COMPUTE_01" "$LOGIN_01"; do
        run docker exec "$c" sinfo -h -o '%P %t %D'
        [ "$status" -eq 0 ]
        [ -n "$output" ]
    done
}

@test "Ansible: srun -N1 hostname from login lands on a slurmd node" {
    run docker exec "$LOGIN_01" srun --partition=debug -N1 hostname
    [ "$status" -eq 0 ]
    [[ "$output" =~ slurm-(compute|controller) ]]
    [[ "$output" != "$LOGIN_01" ]]
}

@test "Ansible: sbatch from login is queued by controller" {
    local job_id
    job_id=$(docker exec "$LOGIN_01" bash -c \
        "sbatch --parsable --partition=debug --wrap='hostname; sleep 1'" 2>&1 | tail -1 | tr -d '[:space:]')
    [ -n "$job_id" ]
    [[ "$job_id" =~ ^[0-9]+$ ]]
    run docker exec "$CONTROLLER" scontrol show job "$job_id"
    [ "$status" -eq 0 ]
    docker exec "$LOGIN_01" scancel "$job_id" 2>/dev/null || true
}

##############################
# Role validators (cedana / cedana-slurm)
##############################

@test "Roles: cedana-slurm setup --node-role login is a no-op" {
    run docker exec "$LOGIN_01" cedana-slurm setup --node-role login
    [ "$status" -eq 0 ]
    [[ "$output" == *login* ]]
}

@test "Roles: cedana-slurm setup --node-role bogus is rejected" {
    run docker exec "$LOGIN_01" cedana-slurm setup --node-role bogus
    [ "$status" -ne 0 ]
    [[ "$output" == *invalid* ]] || [[ "$output" == *"controller"* ]]
}

@test "Roles: cedana-slurm setup with no --node-role is rejected" {
    run docker exec "$LOGIN_01" cedana-slurm setup
    [ "$status" -ne 0 ]
    [[ "$output" == *required* ]] || [[ "$output" == *"node role"* ]]
}

@test "Roles: cedana slurm setup --node-role login is a no-op" {
    run docker exec \
        -e CEDANA_PLUGINS_LIB_DIR=/usr/local/lib \
        -e CEDANA_PLUGINS_BIN_DIR=/usr/local/bin \
        "$LOGIN_01" cedana slurm setup --node-role login
    [ "$status" -eq 0 ]
    [[ "$output" == *login* ]]
}
