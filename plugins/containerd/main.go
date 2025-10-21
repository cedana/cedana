package main

import (
	"syscall"

	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/containerd/cmd"
	"github.com/cedana/cedana/plugins/containerd/internal/client"
	"github.com/cedana/cedana/plugins/containerd/internal/defaults"
	"github.com/cedana/cedana/plugins/containerd/internal/filesystem"
	"github.com/cedana/cedana/plugins/containerd/internal/gpu"
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
	DumpCmd     *cobra.Command = cmd.DumpCmd
	RestoreCmd  *cobra.Command = cmd.RestoreCmd
	FreezeCmd   *cobra.Command = cmd.FreezeCmd
	UnfreezeCmd *cobra.Command = cmd.UnfreezeCmd
	RunCmd      *cobra.Command = cmd.RunCmd
	ManageCmd   *cobra.Command = cmd.ManageCmd
	CmdTheme    text.Colors    = style.HighLevelRuntimeColors
)

var HealthChecks types.Checks = types.Checks{
	List: []types.Check{
		client.CheckVersion(),
		client.CheckRuntime(),
	},
}

var (
	RunHandler    types.Run                   = client.Run
	RunMiddleware types.Middleware[types.Run] = types.Middleware[types.Run]{
		defaults.FillMissingRunDefaults,
		validation.ValidateRunRequest,
		client.SetupForRun,
		client.CreateContainerForRun,
	}
	ManageHandler types.Run = client.Manage

	KillSignal = syscall.SIGKILL
	Cleanup    = client.Cleanup

	GPUInterception        types.Adapter[types.Run]     = gpu.Interception
	// GPUInterceptionRestore types.Adapter[types.Restore] = nil // Handled by lower-level runtime plugin
	GPUTracing             types.Adapter[types.Run]     = gpu.Tracing
	// GPUTracingRestore      types.Adapter[types.Restore] = nil // Handled by lower-level runtime plugin

	FreezeHandler   types.Freeze   = runtime.Freeze
	UnfreezeHandler types.Unfreeze = runtime.Unfreeze

	DumpMiddleware types.Middleware[types.Dump] = types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,
		client.SetupForDump,
		client.LoadContainerForDump,
		filesystem.DumpRootfs,
		filesystem.DumpImageName,

		runtime.DumpMiddleware, // Simply plug in the low-level runtime's dump middleware for the rest
	}

	RestoreHandler    types.Restore                   = client.Restore
	RestoreMiddleware types.Middleware[types.Restore] = types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,
		client.SetupForRestore,
		client.CreateContainerForRestore,
	}
)
