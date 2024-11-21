package flags

import "github.com/cedana/cedana/pkg/flags"

// This file contains all the flags used in this plugin's cmd package.

var (
	RootFlag   = flags.Flag{Full: "root", Short: "r"}
	BundleFlag = flags.Flag{Full: "bundle", Short: "b"}
)
