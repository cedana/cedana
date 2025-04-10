package flags

import "github.com/cedana/cedana/pkg/flags"

var (
	DirFlag      = flags.Flag{Full: "dir", Short: "d"}
	VmTypeFlag   = flags.Flag{Full: "type"}
	PortFlag     = flags.Flag{Full: "port", Short: "p"}
	VmSocketFlag = flags.Flag{Full: "socket"}
)
