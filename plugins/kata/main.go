package main

import (
	"syscall"

	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/kata/cmd"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var (
	DumpVMCmd *cobra.Command = cmd.DumpCmd
	CmdTheme  text.Colors    = style.LowLevelRuntimeColors
)

var KillSignal = syscall.SIGKILL

var HealthChecks types.Checks = types.Checks{
	List: []types.Check{},
}

var (
	DumpVMMiddleware types.Middleware[types.DumpVM] = types.Middleware[types.DumpVM]{}
)
