package keys

const (
	DUMP_IMAGE_NAME_KEY  = "containerd.image"
	DUMP_SNAPSHOT_KEY    = "containerd.snapshot"
	DUMP_SNAPSHOTTER_KEY = "containerd.snapshotter"
	DUMP_RUNTIME_KEY     = "containerd.runtime"

	// The runtime binary (BinaryName runtime option), for runtimes that run
	// under another runtime's shim (e.g. crun under io.containerd.runc.v2)
	DUMP_RUNTIME_BINARY_KEY = "containerd.runtime.binary"
)
