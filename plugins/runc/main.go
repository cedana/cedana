package main

import (
	"syscall"

	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/runc/cmd"
	"github.com/cedana/cedana/plugins/runc/internal/cgroup"
	"github.com/cedana/cedana/plugins/runc/internal/container"
	"github.com/cedana/cedana/plugins/runc/internal/defaults"
	"github.com/cedana/cedana/plugins/runc/internal/device"
	"github.com/cedana/cedana/plugins/runc/internal/filesystem"
	"github.com/cedana/cedana/plugins/runc/internal/gpu"
	"github.com/cedana/cedana/plugins/runc/internal/namespace"
	"github.com/cedana/cedana/plugins/runc/internal/network"
	"github.com/cedana/cedana/plugins/runc/internal/process"
	"github.com/cedana/cedana/plugins/runc/internal/validation"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/opencontainers/runc/libcontainer/configs"
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

var HealthChecks types.Checks = types.Checks{
	List: []types.Check{
		container.CheckBinary(),
		container.CheckVersion(),
	},
}

var QueryHandler types.Query = container.Query

var (
	RunHandler    types.Run                   = container.Run
	RunMiddleware types.Middleware[types.Run] = types.Middleware[types.Run]{
		defaults.FillMissingRunDefaults,
		validation.ValidateRunRequest,
		container.LoadSpecFromBundle,
		process.SetUsChildSubReaper,
	}
	RunDaemonlessSupport bool = true
	KillSignal                = syscall.SIGKILL
	Cleanup                   = container.Cleanup
	Reaper                    = true // we handle our reaping on our own

	ManageHandler types.Run = container.Manage

	GPUInterception types.Adapter[types.Run] = gpu.Interception

	DumpMiddleware types.Middleware[types.Dump] = types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,
		container.GetContainerForDump,

		namespace.IgnoreNamespacesForDump(configs.NEWNET),
		namespace.AddExternalNamespacesForDump(configs.NEWNET, configs.NEWPID),
		filesystem.AddMountsForDump,
		filesystem.AddMaskedPathsForDump,
		cgroup.UseCgroupFreezerIfAvailableForDump,
		device.AddDevicesForDump,
		network.LockNetworkBeforeDump,

		container.SetPIDForDump,
	}

	RestoreMiddleware types.Middleware[types.Restore] = types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,

		container.LoadSpecFromBundleForRestore,
		gpu.RestoreInterceptionIfNeeded,
		container.CreateContainerForRestore,
		filesystem.MountRootDirForRestore,
		filesystem.SetupMountsForRestore,
		filesystem.AddMountsForRestore,
		filesystem.AddMaskedPathsForRestore,
		namespace.IgnoreNamespacesForRestore(configs.NEWNET),
		namespace.InheritExternalNamespacesForRestore(configs.NEWNET, configs.NEWPID),
		namespace.JoinOtherExternalNamespacesForRestore,
		device.AddDevicesForRestore,
		device.HandleEvasiveDevicesForRestore,

		network.RestoreNetworkConfiguration,
		network.UnlockNetworkAfterRestore,
		cgroup.ApplyCgroupsOnRestore,
		container.RunHooksOnRestore,
		container.RestoreConsole,
		container.UpdateStateOnRestore,
		process.SetUsChildSubReaperForRestore,
	}
)
