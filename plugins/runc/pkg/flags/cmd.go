package flags

// This file contains all the flags used in this plugin's cmd package.

import "github.com/cedana/cedana/pkg/flags"

var (
	IdFlag            = flags.Flag{Full: "id", Short: "i"}
	RootFlag          = flags.Flag{Full: "root", Short: "r"}
	DetachFlag        = flags.Flag{Full: "detach", Short: "d"}
	BundleFlag        = flags.Flag{Full: "bundle", Short: "b"}
	NoPivotFlag       = flags.Flag{Full: "no-pivot", Short: ""}
	NoNewKeyringFlag  = flags.Flag{Full: "no-new-keyring"}
	ConsoleSocketFlag = flags.Flag{Full: "console-socket"}
	LogFlag           = flags.Flag{Full: "log"}
	LogFormatFlag     = flags.Flag{Full: "log-format"}
)
