package main

import (
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/runc/cmd"
	"github.com/cedana/cedana/plugins/runc/internal/adapters"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/spf13/cobra"
)

// loaded from ldflag definitions
var Version = "dev"

// CMDs
var (
	RootCmd    *cobra.Command
	DumpCmd    *cobra.Command
	RestoreCmd *cobra.Command
)

// Middleware
var (
	DumpMiddleware    types.Middleware[types.Dump]
	RestoreMiddleware types.Middleware[types.Restore]
)

func init() {
	RootCmd = cmd.RootCmd
	DumpCmd = cmd.DumpCmd
	RestoreCmd = cmd.RestoreCmd

	// NOTE: Assumes other basic request details will be validated by the daemon.
	// Most adapters below are simply lifted from libcontainer/criu_linux.go, which
	// is how official runc binary does a checkpoint. But here, since CRIU C/R is
	// handled by the daemon, this plugin is only responsible for doing setup.

	DumpMiddleware = types.Middleware[types.Dump]{
		// Basic adapters
		adapters.FillMissingDumpDefaults,
		adapters.ValidateDumpRequest,
		adapters.GetContainerForDump,

		// Container-specific adapters
		adapters.AddExternalNamespacesForDump(configs.NEWNET),
		adapters.AddExternalNamespacesForDump(configs.NEWPID),
		adapters.AddBindMountsForDump,
		adapters.AddDevicesForDump,
		adapters.AddMaskedPathsForDump,
		adapters.ManageCgroupsForDump,
		adapters.UseCgroupFreezerIfAvailableForDump,
		adapters.WriteExtDescriptorsForDump,

		// Final adapters
		adapters.SetPIDForDump,
	}

	RestoreMiddleware = types.Middleware[types.Restore]{}
}
