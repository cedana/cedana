package main

import (
	"syscall"

	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/runc/cmd"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var (
	DumpCmd    *cobra.Command = cmd.DumpCmd
	RestoreCmd *cobra.Command = cmd.RestoreCmd
	RunCmd     *cobra.Command = cmd.RunCmd
	ManageCmd  *cobra.Command = cmd.ManageCmd
	QueryCmd   *cobra.Command = cmd.QueryCmd
	CmdTheme   text.Colors    = style.LowLevelRuntimeColors
)

var KillSignal = syscall.SIGKILL

var HealthChecks types.Checks = types.Checks{
	List: []types.Check{},
}

var (
	DumpMiddleware types.Middleware[types.Dump] = types.Middleware[types.Dump]{}

	RestoreMiddleware types.Middleware[types.Restore] = types.Middleware[types.Restore]{}
)
