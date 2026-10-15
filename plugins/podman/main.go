package main

// The podman plugin. Dumps are delegated to the plugin of the container's
// low-level runtime (crun or runc). Runs and restores go through podman, with
// cedana-oci-runtime (the `oci-runtime` plugin) as the container's OCI runtime,
// which routes the container creation to `cedana run/restore <runtime>`. This
// is analogous to how the containerd plugin uses the cedana containerd shim.

import (
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/podman/cmd"
	"github.com/cedana/cedana/plugins/podman/internal/client"
	"github.com/cedana/cedana/plugins/podman/internal/defaults"
	"github.com/cedana/cedana/plugins/podman/internal/gpu"
	"github.com/cedana/cedana/plugins/podman/internal/runtime"
	"github.com/cedana/cedana/plugins/podman/internal/validation"
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
		client.CreateContainer,
	}
	ManageHandler types.Run = client.Manage

	KillSignal             = syscall.SIGKILL
	Cleanup                = client.Cleanup
	ServerlessSupport bool = false

	GPUInterception        types.Adapter[types.Run]     = gpu.Interception
	GPUInterceptionRestore types.Adapter[types.Restore] = gpu.Interception
	GPUTracing             types.Adapter[types.Run]     = gpu.Tracing
	GPUTracingRestore      types.Adapter[types.Restore] = gpu.Tracing

	FreezeHandler   types.Freeze   = runtime.Freeze
	UnfreezeHandler types.Unfreeze = runtime.Unfreeze

	DumpMiddleware types.Middleware[types.Dump] = types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,
		client.LoadContainer[daemon.DumpReq, daemon.DumpResp],
		client.DumpImageName,

		runtime.DumpMiddleware, // Simply plug in the low-level runtime's dump middleware for the rest
	}

	RestoreHandler    types.Restore                   = client.Run // Can simply use Run as cedana-oci-runtime will handle restoring
	RestoreMiddleware types.Middleware[types.Restore] = types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,
		client.CreateContainerForRestore,
	}
)
