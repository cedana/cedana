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
		Binaries: []string{"cedana-criu"},
	},
	// TODO: can add hypervisor C/R tools

	// Container runtimes
	{
		Name:      "runc",
		Type:      Supported,
		Libraries: []string{"libcedana-runc.so"},
	},
	{
		Name:         "containerd",
		Libraries:    []string{"libcedana-containerd.so"},
		Dependencies: []string{"runc"},
	},
	{
		Name:         "crio",
		Libraries:    []string{"libcedana-crio.so"},
		Dependencies: []string{"runc"},
	},
	{
		Name:      "kata",
		Libraries: []string{"libcedana-kata.so"},
	},
	{
		Name:      "docker",
		Libraries: []string{"libcedana-docker.so"},
	},

	// Checkpoint/Restore
	{
		Name:      "gpu",
		Type:      External,
		Libraries: []string{"libcedana-gpu.so"},
		Binaries:  []string{"cedana-gpu-controller"},
	},
	{
		Name:      "streamer",
		Type:      Experimental,
		Libraries: []string{"libcedana-streamer.so"},
		Binaries:  []string{"cedana-image-streamer"},
	},

	// Checkpoint inspection
	{
		Name:      "inspector",
		Type:      Supported,
		Libraries: []string{"libcedana-inspector.so"},
	},
}
