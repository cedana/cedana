package features

// Defines all the supported features by the daemon
// that plugins can export

import (
	"context"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

var (
	// Commands
	CmdTheme   = plugins.Feature[text.Colors]{Symbol: "CmdTheme", Description: "Theme for commands"}
	DumpCmd    = plugins.Feature[*cobra.Command]{Symbol: "DumpCmd", Description: "Dump command"}
	DumpVMCmd  = plugins.Feature[*cobra.Command]{Symbol: "DumpVMCmd", Description: "Dump VM command"}
	RestoreCmd = plugins.Feature[*cobra.Command]{Symbol: "RestoreCmd", Description: "Restore command"}
	RunCmd     = plugins.Feature[*cobra.Command]{Symbol: "RunCmd", Description: "Run command"}
	ManageCmd  = plugins.Feature[*cobra.Command]{Symbol: "ManageCmd", Description: "Manage command"}
	QueryCmd   = plugins.Feature[*cobra.Command]{Symbol: "QueryCmd", Description: "Query command"}
	HelperCmds = plugins.Feature[[]*cobra.Command]{Symbol: "HelperCmds", Description: "Helper command(s)"}

	// Dump/Restore
	DumpMiddleware      = plugins.Feature[types.Middleware[types.Dump]]{Symbol: "DumpMiddleware", Description: "Dump middleware"}
	DumpVMMiddleware    = plugins.Feature[types.Middleware[types.DumpVM]]{Symbol: "DumpVMMiddleware", Description: "Dump VM middleware"}
	DumpVMHandler       = plugins.Feature[types.DumpVM]{Symbol: "DumpVMHandler", Description: "Dump VM handler"}
	RestoreMiddleware   = plugins.Feature[types.Middleware[types.Restore]]{Symbol: "RestoreMiddleware", Description: "Restore middleware"}
	RestoreVMMiddleware = plugins.Feature[types.Middleware[types.RestoreVM]]{Symbol: "RestoreVMMiddleware", Description: "Restore VM middleware"}
	RestoreVMHandler    = plugins.Feature[types.RestoreVM]{Symbol: "RestoreVMHandler", Description: "Restore VM handler"}

	// Run
	RunHandler           = plugins.Feature[types.Run]{Symbol: "RunHandler", Description: "Run handler"}
	RunDaemonlessSupport = plugins.Feature[bool]{Symbol: "RunDaemonlessSupport", Description: "Run handler (daemonless)"}
	RunMiddleware        = plugins.Feature[types.Middleware[types.Run]]{Symbol: "RunMiddleware", Description: "Run middleware"}
	KillSignal           = plugins.Feature[syscall.Signal]{Symbol: "KillSignal", Description: "Custom kill signal"}
	Cleanup              = plugins.Feature[func(details *daemon.Details) error]{Symbol: "Cleanup", Description: "Custom cleanup"}
	Reaper               = plugins.Feature[bool]{Symbol: "Reaper", Description: "Custom reaper"}

	// Manage
	ManageHandler = plugins.Feature[types.Run]{Symbol: "ManageHandler", Description: "Manage handler"}

	// GPU
	GPUInterception = plugins.Feature[types.Adapter[types.Run]]{Symbol: "GPUInterception", Description: "GPU interception"}

	// Query
	QueryHandler = plugins.Feature[types.Query]{Symbol: "QueryHandler", Description: "Query handler"}

	// Health check
	HealthChecks = plugins.Feature[types.Checks]{Symbol: "HealthChecks", Description: "Health checks"}

	// Storage
	Storage = plugins.Feature[func(context.Context) (io.Storage, error)]{Symbol: "NewStorage", Description: "Checkpoint storage"}
)
