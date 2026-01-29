#!/usr/bin/env bats

# bats file_tags=k8s,kubernetes,cpu,large

load ../helpers/utils
load ../helpers/daemon # required for config env vars
load ../helpers/k8s
load ../helpers/helm
load ../helpers/propagator

#####################################################
# Large Cedana Samples (ML Training, Scientific HPC) #
#####################################################

# bats test_tags=dump,restore,samples,ml,xgboost
@test "Dump/Restore: XGBoost Training" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/xgboost-training.yaml" "$NAMESPACE" "xgboost")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE" 
}

# bats test_tags=dump,restore,samples,ml,sklearn,random-forest
@test "Dump/Restore: Scikit-learn Random Forest" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/sklearn-random-forest.yaml" "$NAMESPACE" "sklearn-rf")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE" 
}

# bats test_tags=dump,restore,samples,simulation,monte-carlo
@test "Dump/Restore: Monte Carlo Pi Estimation" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/monte-carlo-pi.yaml" "$NAMESPACE" "monte-carlo")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 600 60 300 "$NAMESPACE" 
}

# bats test_tags=dump,restore,samples,hpc,numpy
@test "Dump/Restore: NumPy Matrix Operations" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/numpy-matrix-ops.yaml" "$NAMESPACE" "numpy")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE" 
}

# bats test_tags=dump,restore,samples,hpc,linpack
@test "Dump/Restore: HPL Linpack Benchmark" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/hpl-linpack.yaml" "$NAMESPACE" "linpack")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE" 
}

# bats test_tags=dump,restore,samples,compbio,lammps
@test "Dump/Restore: LAMMPS Molecular Dynamics" {
    local spec
    spec=$(pod_spec "$SAMPLES_DIR/cpu/lammps-molecular-dynamics.yaml" "$NAMESPACE" "lammps")

    test_pod_spec DEPLOY_DUMP_RESTORE "$spec" 900 60 300 "$NAMESPACE"  
}
