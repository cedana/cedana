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
	CmdTheme   = plugins.Feature[text.Colors]{Symbol: "CmdTheme", Description: "theme for commands"}
	DumpCmd    = plugins.Feature[*cobra.Command]{Symbol: "DumpCmd", Description: "dump command"}
	RestoreCmd = plugins.Feature[*cobra.Command]{Symbol: "RestoreCmd", Description: "restore command"}
	RunCmd     = plugins.Feature[*cobra.Command]{Symbol: "RunCmd", Description: "run command"}
	RootCmds   = plugins.Feature[[]*cobra.Command]{Symbol: "RootCmds", Description: "root commands"}

	GPUInterception = plugins.Feature[types.Adapter[types.Run]]{Symbol: "GPUInterception", Description: "GPU interception"}

	RunHandler    = plugins.Feature[types.Run]{Symbol: "RunHandler", Description: "Run handler"}
	RunMiddleware = plugins.Feature[types.Middleware[types.Run]]{Symbol: "RunMiddleware", Description: "run middleware"}
	KillSignal    = plugins.Feature[syscall.Signal]{Symbol: "KillSignal", Description: "signal to use for killing the process"}

	DumpMiddleware    = plugins.Feature[types.Middleware[types.Dump]]{Symbol: "DumpMiddleware", Description: "dump middleware"}
	RestoreMiddleware = plugins.Feature[types.Middleware[types.Restore]]{Symbol: "RestoreMiddleware", Description: "restore middleware"}
	CheckpointInfo    = plugins.Feature[func(path string, imgType string) ([]byte, error)]{Symbol: "CheckpointInfo", Description: "get checkpoint info"}
)
