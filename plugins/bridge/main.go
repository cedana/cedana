package main

import (
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/plugins/bridge/cmd"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var (
	HelperCmds []*cobra.Command = []*cobra.Command{cmd.HelperCmd}
	CmdTheme   text.Colors      = style.HighLevelRuntimeColors
)
