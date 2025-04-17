package main

import (
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/cloud-hypervisor/cmd"
	"github.com/cedana/cedana/plugins/cloud-hypervisor/internal/handlers"

	"github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/filesystem"
	"github.com/spf13/cobra"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var (
	DumpVMCmd *cobra.Command = cmd.DumpCmd
	RunCmd    *cobra.Command = cmd.RunCmd
	// RestoreVMCmd *cobra.Command = cmd.RestoreCmd
)

var (
	RunHandler       types.Run                      = handlers.Run
	RunMiddleware    types.Middleware[types.Run]    = types.Middleware[types.Run]{}
	DumpVMHandler    types.DumpVM                   = handlers.Dump
	RestoreVMHandler types.RestoreVM                = handlers.Restore
	DumpVMMiddleware types.Middleware[types.DumpVM] = types.Middleware[types.DumpVM]{
		filesystem.PrepareDumpDir,
	}
	RestoreVMMiddleware types.Middleware[types.RestoreVM] = types.Middleware[types.RestoreVM]{
		filesystem.PrepareDumpDirForRestore,
	}
)
