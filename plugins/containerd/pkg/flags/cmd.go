package flags

// This file contains all the flags used in this plugin's cmd package.

import "github.com/cedana/cedana/pkg/flags"

var (
	NamespaceFlag    = flags.Flag{Full: "namespace"}
	AddressFlag      = flags.Flag{Full: "address"}
	ImageFlag        = flags.Flag{Full: "image"}
	RootfsFlag       = flags.Flag{Full: "rootfs"}
	RootfsOnlyFlag   = flags.Flag{Full: "rootfs-only"}
	GPUsFlag         = flags.Flag{Full: "gpus"}
	EnvFlag          = flags.Flag{Full: "env"}
	SnapshottterFlag = flags.Flag{Full: "snapshotter"}
)
