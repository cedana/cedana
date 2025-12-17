package main

import (
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
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
	QueryCmd    *cobra.Command = cmd.QueryCmd
	CmdTheme    text.Colors    = style.HighLevelRuntimeColors
)

var HealthChecks types.Checks = types.Checks{
	List: []types.Check{
		client.CheckVersion(),
		client.CheckRuntime(),
	},
}

var QueryHandler types.Query = client.Query

var (
	RunHandler    types.Run                   = client.Run
	RunMiddleware types.Middleware[types.Run] = types.Middleware[types.Run]{
		defaults.FillMissingRunDefaults,
		validation.ValidateRunRequest,
		client.Setup[daemon.RunReq, daemon.RunResp],
		client.CreateContainer,
		client.SetAdditionalEnv[daemon.RunReq, daemon.RunResp],
	}
	ManageHandler types.Run = client.Manage

	KillSignal = syscall.SIGKILL
	Cleanup    = client.Cleanup

	GPUInterception        types.Adapter[types.Run]     = gpu.Interception
	GPUInterceptionRestore types.Adapter[types.Restore] = gpu.Interception
	GPUTracing             types.Adapter[types.Run]     = gpu.Tracing
	GPUTracingRestore      types.Adapter[types.Restore] = gpu.Tracing

	FreezeHandler   types.Freeze   = runtime.Freeze
	UnfreezeHandler types.Unfreeze = runtime.Unfreeze

	DumpMiddleware types.Middleware[types.Dump] = types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,
		client.Setup[daemon.DumpReq, daemon.DumpResp],
		client.LoadContainer[daemon.DumpReq, daemon.DumpResp],
		filesystem.DumpImageName,
		filesystem.DumpSnapshotter,

		runtime.DumpMiddleware, // Simply plug in the low-level runtime's dump middleware for the rest
	}

	RestoreHandler    types.Restore                   = client.Run // Can simply use Run as shim will handle restoring
	RestoreMiddleware types.Middleware[types.Restore] = types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,
		client.Setup[daemon.RestoreReq, daemon.RestoreResp],
		client.CreateContainerForRestore,
		client.SetAdditionalEnv[daemon.RestoreReq, daemon.RestoreResp],
	}
)
