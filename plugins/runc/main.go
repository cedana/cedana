package main

import (
	"syscall"

	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/runc/cmd"
	"github.com/cedana/cedana/plugins/runc/internal/cgroup"
	"github.com/cedana/cedana/plugins/runc/internal/container"
	"github.com/cedana/cedana/plugins/runc/internal/defaults"
	"github.com/cedana/cedana/plugins/runc/internal/device"
	"github.com/cedana/cedana/plugins/runc/internal/filesystem"
	"github.com/cedana/cedana/plugins/runc/internal/gpu"
	"github.com/cedana/cedana/plugins/runc/internal/namespace"
	"github.com/cedana/cedana/plugins/runc/internal/validation"
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

	Theme text.Colors = text.Colors{text.FgCyan}
)

var (
	RunMiddleware  types.Middleware[types.Run]
	GPUInterceptor types.Adapter[types.Run]
	RunHandler     types.Run

	DumpMiddleware    types.Middleware[types.Dump]
	RestoreMiddleware types.Middleware[types.Restore]
)

var KillSignal syscall.Signal = container.KILL_SIGNAL

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

	// Assuming other basic request details will be validated by the daemon.
	// Most adapters below are simply lifted from libcontainer/criu_linux.go, which
	// is how official runc binary does a checkpoint. But here, since CRIU C/R is
	// handled by the daemon, this plugin is only responsible for doing runc-specific setup.

	DumpMiddleware = types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,
		container.GetContainerForDump,

		namespace.AddExternalNamespacesForDump(configs.NEWNET),
		namespace.AddExternalNamespacesForDump(configs.NEWPID),
		filesystem.AddBindMountsForDump,
		filesystem.AddMaskedPathsForDump,
		device.AddDevicesForDump,
		cgroup.ManageCgroupsForDump,
		cgroup.UseCgroupFreezerIfAvailableForDump,

		container.SetPIDForDump,
	}

	RestoreMiddleware = types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,

		filesystem.SetWorkingDirectoryForRestore,
		container.LoadSpecFromBundleForRestore,
		container.CreateContainerForRestore,

		filesystem.MountRootDirForRestore,
	}

	RunMiddleware = types.Middleware[types.Run]{
		defaults.FillMissingRunDefaults,
		validation.ValidateRunRequest,

		filesystem.SetWorkingDirectory,
		container.LoadSpecFromBundle,
		// Can add other adapters that wish to modify the spec before running
	}

	GPUInterceptor = gpu.GPUInterceptor

	RunHandler = container.Run()
}
