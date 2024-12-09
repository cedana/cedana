package keys

// Defines common keys used in context. Should
// be consulted when adding new keys in a plugin to avoid conflicts.

// NOTE: Do not add plugin keys here. Plugin keys should be
// defined in the plugin's own types package.

const (
	DUMP_REQ_CONTEXT_KEY = iota
	RESTORE_REQ_CONTEXT_KEY
	RUN_REQ_CONTEXT_KEY

	LOG_FILE_CONTEXT_KEY
	EXTRA_FILES_CONTEXT_KEY

	CLIENT_CONTEXT_KEY
	PLUGIN_MANAGER_CONTEXT_KEY
)
