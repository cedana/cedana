package plugins

// Local registry of plugins that are supported by Cedana
// XXX: This list is needed locally so when all plugins are loaded,
// we only want to attempt to load the ones that are 'Supported'.
// GPU plugin, for example, is not 'External' and thus cannot be loaded
// by Go, so we shouldn't try, as it's undefined behavior.
var Registry = []Plugin{
	// C/R tools
	{
		Name:     "criu",
		Type:     External,
		Binaries: []string{"criu"},
	},
	// TODO: can add hypervisor C/R tools

	// Container runtimes
	{
		Name:      "runc",
		Type:      Supported,
		Libraries: []string{"libcedana-runc.so"},
	},
	{
		Name:      "containerd",
		Type:      Supported,
		Libraries: []string{"libcedana-containerd.so"},
	},
	{
		Name:      "crio",
		Type:      Supported,
		Libraries: []string{"libcedana-crio.so"},
	},
	{
		Name:      "kata",
		Libraries: []string{"libcedana-kata.so"},
	},

	// Checkpoint inspection
	{
		Name: "inspector",
		// Type:      Supported,
		Libraries: []string{"libcedana-inspector.so"},
	},

	// Others
	{
		Name:      "gpu",
		Type:      External,
		Libraries: []string{"libcedana-gpu.so"},
		Binaries:  []string{"cedana-gpu-controller"},
	},
	{
		Name:      "streamer",
		Type:      External,
		Libraries: []string{"libcedana-streamer.so"},
		Binaries:  []string{"cedana-image-streamer"},
	},
	{
		Name:      "k8s",
		Type:      Supported,
		Libraries: []string{"libcedana-k8s.so"},
		Binaries:  []string{}, // TODO: add containerd shim binary
	},
}
