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
		Type:     EXTERNAL,
		Binaries: []Binary{{Name: "criu"}},
	},
	{
		Name:      "cloud-hypervisor",
		Type:      SUPPORTED,
		Libraries: []Binary{{Name: "libcedana-cloud-hypervisor.so"}},
	},
	{
		Name:      "criu/cuda",
		Type:      EXTERNAL,
		Binaries:  []Binary{{Name: "cuda-checkpoint"}},
		Libraries: []Binary{{Name: "cuda_plugin.so", InstallDir: "/usr/lib/criu"}},
	},

	// Container runtimes
	{
		Name:      "runc",
		Type:      SUPPORTED,
		Libraries: []Binary{{Name: "libcedana-runc.so"}},
	},
	{
		Name:      "containerd",
		Type:      SUPPORTED,
		Libraries: []Binary{{Name: "libcedana-containerd.so"}},
	},
	{
		Name:      "crio",
		Type:      SUPPORTED,
		Libraries: []Binary{{Name: "libcedana-crio.so"}},
	},
	{
		Name:      "kata",
		Type:      SUPPORTED,
		Libraries: []Binary{{Name: "libcedana-kata.so"}},
	},

	// Checkpoint inspection
	{
		Name: "inspector",
		// Type:      Supported,
		Libraries: []Binary{{Name: "libcedana-inspector.so"}},
	},

	// Others
	{
		Name:      "gpu",
		Type:      EXTERNAL,
		Libraries: []Binary{{Name: "libcedana-gpu.so"}},
		Binaries:  []Binary{{Name: "cedana-gpu-controller"}},
	},
	{
		Name:     "streamer",
		Type:     EXTERNAL,
		Binaries: []Binary{{Name: "cedana-image-streamer"}},
	},
	{
		Name:      "k8s",
		Type:      SUPPORTED,
		Libraries: []Binary{{Name: "libcedana-k8s.so"}},
		Binaries:  []Binary{}, // TODO: add containerd shim binary
	},
	{
		Name:      "k8s/runtime-shim",
		Type:      EXTERNAL,
		Libraries: []Binary{},
		Binaries:  []Binary{{Name: "cedana-shim-runc-v2"}},
	},
}
