package keys

// Defines common keys used in this plugin

type contextKey struct{}

var (
	CGROUP_MANAGER_CONTEXT_KEY = contextKey{}
	SPEC_CONTEXT_KEY           = contextKey{}
	INIT_PROCESS_CONTEXT_KEY   = contextKey{}
	SIGNAL_HANDLER_CONTEXT_KEY = contextKey{}
	TTY_CONTEXT_KEY            = contextKey{}
)
