package main

import (
	"syscall"

	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/types"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/spf13/cobra"

	"github.com/cedana/cedana/plugins/slurm/cmd"
	"github.com/cedana/cedana/plugins/slurm/internal/cgroup"
	"github.com/cedana/cedana/plugins/slurm/internal/defaults"
	"github.com/cedana/cedana/plugins/slurm/internal/job"
	"github.com/cedana/cedana/plugins/slurm/internal/namespaces"
	"github.com/cedana/cedana/plugins/slurm/internal/network"
	"github.com/cedana/cedana/plugins/slurm/internal/validation"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var (
	RestoreCmd *cobra.Command   = cmd.RestoreCmd
	HelperCmds []*cobra.Command = []*cobra.Command{cmd.HelperCmd}
	CmdTheme   text.Colors      = style.HighLevelRuntimeColors
)

var (
	KillSignal = syscall.SIGKILL

	FreezeHandler   types.Freeze   = cgroup.Freeze
	UnfreezeHandler types.Unfreeze = cgroup.Unfreeze

	DumpMiddleware types.Middleware[types.Dump] = types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,

		job.SetPIDForDump,

		job.GetSlurmJobForDump,

		cgroup.UseCgroupFreezerIfAvailableForDump,
		// https://github.com/SchedMD/slurm/blob/035cb8f0b5d1fb6a375b27f2ecde106b84473ed5/src/plugins/namespace/linux/namespace_linux.c#L112-L138
		namespaces.AddExternalNamespacesForDump(configs.NEWNS, configs.NEWPID, configs.NEWUSER),
		network.LockNetworkBeforeDump,
	}

	RestoreMiddleware types.Middleware[types.Restore] = types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,

		job.GetSlurmJobForRestore,

		network.UnlockNetworkAfterRestore,
		cgroup.ApplyCgroupsOnRestore,
		// the 3 nstypes are taken from slurm namespace plugin
		// https://github.com/SchedMD/slurm/blob/035cb8f0b5d1fb6a375b27f2ecde106b84473ed5/src/plugins/namespace/linux/namespace_linux.c#L112-L138
		namespaces.InheritExternalNamespacesForRestore(configs.NEWNS, configs.NEWPID, configs.NEWUSER),
	}
)
