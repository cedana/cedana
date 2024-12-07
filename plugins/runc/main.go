package main

import (
	"syscall"

	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/runc/cmd"
	"github.com/cedana/cedana/plugins/runc/internal/adapters"
	"github.com/cedana/cedana/plugins/runc/internal/handlers"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/spf13/cobra"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version = "dev"

var (
	RootCmds []*cobra.Command

	RunCmd     *cobra.Command
	DumpCmd    *cobra.Command
	RestoreCmd *cobra.Command

	Theme text.Colors = text.Colors{text.FgHiCyan}
)

var (
	RunMiddleware  types.Middleware[types.Run]
	GPUInterceptor types.Adapter[types.Run]
	RunHandler     types.Run

	DumpMiddleware    types.Middleware[types.Dump]
	RestoreMiddleware types.Middleware[types.Restore]
)

var KillSignal syscall.Signal = handlers.KILL_SIGNAL

////////////////////////
//// Initialization ////
////////////////////////

func init() {
	RootCmds = []*cobra.Command{
		cmd.RootCmd,
	}

	DumpCmd = cmd.DumpCmd
	RestoreCmd = cmd.RestoreCmd
	RunCmd = cmd.RunCmd

	// NOTE: Assumes other basic request details will be validated by the daemon.
	// Most adapters below are simply lifted from libcontainer/criu_linux.go, which
	// is how official runc binary does a checkpoint. But here, since CRIU C/R is
	// handled by the daemon, this plugin is only responsible for doing runc-specific setup.

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

	RunMiddleware = types.Middleware[types.Run]{
		adapters.FillMissingRunDefaults,
		adapters.ValidateRunRequest,
		adapters.SetWorkingDirectory,
		adapters.LoadSpecFromBundle,
		// Can add other adapters that wish to modify the spec before running
	}

	GPUInterceptor = adapters.GPUInterceptor

	RunHandler = handlers.Run()
}
