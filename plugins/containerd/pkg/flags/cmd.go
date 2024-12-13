package flags

// This file contains all the flags used in this plugin's cmd package.

import "github.com/cedana/cedana/pkg/flags"

var (
	NamespaceFlag = flags.Flag{Full: "namespace", Short: "n"}
	AddressFlag   = flags.Flag{Full: "address"}
	ImageFlag     = flags.Flag{Full: "image", Short: "i"}
)
