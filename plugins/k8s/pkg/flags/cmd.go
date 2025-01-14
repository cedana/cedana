package flags

// This file contains all the flags used in this plugin's cmd package.

import "github.com/cedana/cedana/pkg/flags"

var (
	NameFlag    = flags.Flag{Full: "name"}
	SandboxFlag = flags.Flag{Full: "sandbox"}
)
