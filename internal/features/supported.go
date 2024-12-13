package features

// Defines all the supported features by the daemon
// that plugins can export

import (
	"syscall"

	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

var (
	// Commands
	CmdTheme   = plugins.Feature[text.Colors]{Symbol: "CmdTheme", Description: "Theme for commands"}
	DumpCmd    = plugins.Feature[*cobra.Command]{Symbol: "DumpCmd", Description: "Dump command"}
	RestoreCmd = plugins.Feature[*cobra.Command]{Symbol: "RestoreCmd", Description: "Restore command"}
	RunCmd     = plugins.Feature[*cobra.Command]{Symbol: "RunCmd", Description: "Run command"}
	RootCmds   = plugins.Feature[[]*cobra.Command]{Symbol: "RootCmds", Description: "Root commands"}

	// Run
	RunHandler    = plugins.Feature[types.Run]{Symbol: "RunHandler", Description: "Run handler"}
	RunMiddleware = plugins.Feature[types.Middleware[types.Run]]{Symbol: "RunMiddleware", Description: "Run middleware"}

	// Dump/Restore
	DumpMiddleware    = plugins.Feature[types.Middleware[types.Dump]]{Symbol: "DumpMiddleware", Description: "Dump middleware"}
	RestoreMiddleware = plugins.Feature[types.Middleware[types.Restore]]{Symbol: "RestoreMiddleware", Description: "Restore middleware"}

	// Other
	KillSignal        = plugins.Feature[syscall.Signal]{Symbol: "KillSignal", Description: "Custom kill signal"}
	GPUInterception   = plugins.Feature[types.Adapter[types.Run]]{Symbol: "GPUInterception", Description: "GPU interception"}
	CheckpointInspect = plugins.Feature[func(path string, imgType string) ([]byte, error)]{Symbol: "CheckpointInspect", Description: "Checkpoint inspect"}
	CheckpointDecode  = plugins.Feature[func(path string, imgType string) ([]byte, error)]{Symbol: "CheckpointDecode", Description: "Checkpoint decode"}
	CheckpointEncode  = plugins.Feature[func(path string, imgType string) ([]byte, error)]{Symbol: "CheckpointEncode", Description: "Checkpoint encode"}
)
