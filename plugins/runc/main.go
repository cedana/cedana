package main

import (
	"syscall"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
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
	RootCmds = []*cobra.Command{
		cmd.RootCmd,
	}
	DumpCmd    = cmd.DumpCmd
	RestoreCmd = cmd.RestoreCmd
	RunCmd     = cmd.RunCmd
	Theme      = text.Colors{text.FgCyan}
)

var KillSignal = syscall.SIGKILL

var (
	RunHandler     = container.Run()
	GPUInterceptor = gpu.Interceptor
	RunMiddleware  = types.Middleware[types.Run]{
		defaults.FillMissingRunDefaults,
		validation.ValidateRunRequest,
		filesystem.SetWorkingDirectory,
		container.LoadSpecFromBundle,
	}

	DumpMiddleware = types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,
		container.GetContainerForDump,

		namespace.IgnoreNamespacesForDump(configs.NEWNET),
		namespace.AddExternalNamespacesForDump(configs.NEWNET, configs.NEWPID),
		filesystem.AddMountsForDump,
		filesystem.AddMaskedPathsForDump,
		cgroup.ManageCgroupsForDump(criu.CriuCgMode_SOFT),
		cgroup.UseCgroupFreezerIfAvailableForDump,
		device.AddDevicesForDump,

		container.SetPIDForDump,
	}

	RestoreMiddleware = types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,
		filesystem.SetWorkingDirectoryForRestore,

		container.LoadSpecFromBundleForRestore,
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

		network.RestoreNetwork,
		cgroup.ManageCgroupsForRestore(criu.CriuCgMode_SOFT),
		cgroup.ApplyCgroupsOnRestore,
		container.RunHooksOnRestore,
		container.UpdateStateOnRestore,
		container.CleanupOnExitAfterRestore,
	}
)
