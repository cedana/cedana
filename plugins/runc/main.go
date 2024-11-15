package main

import (
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/runc/cmd"
	"github.com/cedana/cedana/plugins/runc/internal/adapters"
	"github.com/spf13/cobra"
)

// loaded from ldflag definitions
var Version = "dev"

// CMDs
var (
	DumpCmd    *cobra.Command
	RestoreCmd *cobra.Command
)

// Middleware
var (
	DumpMiddleware    types.Middleware[types.Dump]
	RestoreMiddleware types.Middleware[types.Restore]
)

func init() {
	DumpCmd = cmd.DumpCmd
	RestoreCmd = cmd.RestoreCmd

	// NOTE: Assumes other basic request details will be validated by the daemon

	DumpMiddleware = types.Middleware[types.Dump]{
		adapters.FillMissingDumpDefaults,
		adapters.ValidateDumpRequest,
		adapters.GetContainerForDump,
	}

	RestoreMiddleware = types.Middleware[types.Restore]{}
}
