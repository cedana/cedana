package scripts

import _ "embed"

//go:embed install-service.sh
var InstallService string

//go:embed reset-service.sh
var ResetService string

//go:embed install-deps.sh
var InstallDeps string

//go:embed configure-shm.sh
var ConfigureShm string

//go:embed configure-io-uring.sh
var ConfigureIoUring string

// ConfigureCheckpointTmpfs provides /checkpoint as tmpfs, the memory storage
// tier for checkpoints. Gated behind CHECKPOINT_TMPFS_ENABLED and a ceiling on
// the share of host RAM it may claim, because tmpfs pages compete with the
// workload's own memory.
//
//go:embed configure-checkpoint-tmpfs.sh
var ConfigureCheckpointTmpfs string

//go:embed utils.sh
var Utils string
