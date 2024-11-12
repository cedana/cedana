package types

import "github.com/cedana/cedana/pkg/types"

// This file contains all the flags used in this plugin's cmd package.

var (
	RootFlag   = types.Flag{Full: "root", Short: "r"}
	BundleFlag = types.Flag{Full: "bundle", Short: "b"}
)
