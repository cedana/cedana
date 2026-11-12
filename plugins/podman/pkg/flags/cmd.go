package flags

// This file contains all the flags used in this plugin's cmd package.

import "github.com/cedana/cedana/pkg/flags"

var (
	ImageFlag = flags.Flag{Full: "image"}
	GPUsFlag  = flags.Flag{Full: "gpus"}
	EnvFlag   = flags.Flag{Full: "env"}
)
