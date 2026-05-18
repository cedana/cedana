package keys

// Defines common keys used in context. Should
// be consulted when adding new keys in a plugin to avoid conflicts.

// NOTE: Do not add plugin keys here. Plugin keys should be
// defined in the plugin's own types package.

const (
	DUMP_REQ_CONTEXT_KEY = iota
	RESTORE_REQ_CONTEXT_KEY
	RUN_REQ_CONTEXT_KEY
	QUERY_REQ_CONTEXT_KEY
	QUERY_OUTPUT_CONTEXT_KEY
	QUERY_RESP_CONTEXT_KEY
	FREEZE_REQ_CONTEXT_KEY
	UNFREEZE_REQ_CONTEXT_KEY

	GPU_ID_CONTEXT_KEY
	GPU_LOG_DIR_CONTEXT_KEY
	EXIT_CODE_CHANNEL_CONTEXT_KEY

	CLIENT_CONTEXT_KEY
	PLUGIN_MANAGER_CONTEXT_KEY
	PROFILING_CONTEXT_KEY
)
