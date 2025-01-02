package main

import (
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/containerd/cmd"
	"github.com/cedana/cedana/plugins/containerd/internal/client"
	"github.com/cedana/cedana/plugins/containerd/internal/defaults"
	"github.com/cedana/cedana/plugins/containerd/internal/filesystem"
	"github.com/cedana/cedana/plugins/containerd/internal/runtime"
	"github.com/cedana/cedana/plugins/containerd/internal/validation"
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
	CmdTheme   text.Colors    = text.Colors{text.FgMagenta}
)

var (
	RunHandler    types.Run                   = nil
	RunMiddleware types.Middleware[types.Run] = types.Middleware[types.Run]{}

	DumpMiddleware types.Middleware[types.Dump] = types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequst,
		client.SetupForDump,
		filesystem.AddRootfsToDump,

		runtime.DumpMiddleware, // Simply plug in the runtime's dump middleware for the rest
	}

	RestoreMiddleware types.Middleware[types.Restore] = types.Middleware[types.Restore]{}
)
