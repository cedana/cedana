package keys

// Container annotations read by cedana-oci-runtime, the OCI runtime that
// podman uses for containers run/restored by cedana. Keep in sync with
// https://github.com/cedana/cedana-oci-runtime.

const (
	CHECKPOINT_PATH_ANNOTATION = "cedana.ai/checkpoint-path" // restore from this checkpoint instead of creating
	CRIU_OPTS_ANNOTATION       = "cedana.ai/criu-opts"       // JSON (or base64-encoded JSON) CRIU options for the restore
	CONFIG_ANNOTATION          = "cedana.ai/config"          // JSON (or base64-encoded JSON) cedana config to call cedana with
	EXECUTABLE_PATH_ANNOTATION = "cedana.ai/executable-path" // cedana binary, set when a parent cedana manages the container
	RUNTIME_ANNOTATION         = "cedana.ai/runtime"         // real runtime (crun or runc)
	GPU_ANNOTATION             = "cedana.ai/gpu"             // enable GPU interception
	GPU_TRACING_ANNOTATION     = "cedana.ai/gpu-tracing"     // enable GPU tracing
	GPU_ID_ANNOTATION          = "cedana.ai/gpu-id"          // GPU controller to use, if already attached
)
