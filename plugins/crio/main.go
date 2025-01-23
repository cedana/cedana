package main

import (
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/crio/cmd"
	"github.com/cedana/cedana/plugins/crio/internal/client"
	"github.com/cedana/cedana/plugins/crio/internal/defaults"
	"github.com/cedana/cedana/plugins/crio/internal/filesystem"
	"github.com/cedana/cedana/plugins/crio/internal/validation"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var CmdTheme text.Colors = style.HighLevelRuntimeColors

var (
	DumpCmd *cobra.Command = cmd.DumpCmd
)

var HealthChecks types.Checks = types.Checks{
	List: []types.Check{
		client.CheckVersion(),
		client.CheckRuntime(),
	},
}

// CRIO doesn't support run or restore handlers (atleast it's not worth adding them for just consistency)
var (
	// RunHandler    types.Run                   = nil
	// RunMiddleware types.Middleware[types.Run] = types.Middleware[types.Run]{}

	DumpMiddleware types.Middleware[types.Dump] = types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,
		client.SetupForDump,
		filesystem.DumpRootfs,
	}

	// RestoreMiddleware types.Middleware[types.Restore] = types.Middleware[types.Restore]{}
)
