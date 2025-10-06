package main

import (
	"syscall"

	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/types"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"github.com/cedana/cedana/plugins/slurm/cmd"
	"github.com/cedana/cedana/plugins/slurm/internal/cgroup"
	"github.com/cedana/cedana/plugins/slurm/internal/defaults"
	"github.com/cedana/cedana/plugins/slurm/internal/job"
	"github.com/cedana/cedana/plugins/slurm/internal/network"
	"github.com/cedana/cedana/plugins/slurm/internal/validation"
)

///////////////////////////
//// Exported Features ////
///////////////////////////

// loaded from ldflag definitions
var Version string = "dev"

var (
	DumpCmd  *cobra.Command = cmd.DumpCmd
	CmdTheme text.Colors    = style.LowLevelRuntimeColors
)

// TODO: add health checks for slurmctld and slurmd daemons
// var HealthChecks types.Checks = types.Checks{
// 	List: []types.Check{},
// }

// TODO: implement query
// var QueryHandler types.Query = slurm.Query

var (
	KillSignal = syscall.SIGKILL
	// Cleanup    = slurm.Cleanup // TODO: maybe implement cleanup if slurm doesn't handle it properly
	Reaper = true // we handle our reaping on our own

	FreezeHandler   types.Freeze   = cgroup.Freeze
	UnfreezeHandler types.Unfreeze = cgroup.Unfreeze

	DumpMiddleware types.Middleware[types.Dump] = types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,

		job.GetSlurmJobForDump,

		cgroup.UseCgroupFreezerIfAvailableForDump,
		network.LockNetworkBeforeDump,

		job.SetPIDForDump,
	}

	RestoreMiddleware types.Middleware[types.Restore] = types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,

		job.GetSlurmJobForRestore,

		network.UnlockNetworkAfterRestore,
		cgroup.ApplyCgroupsOnRestore,
	}
)
