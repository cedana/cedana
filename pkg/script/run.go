package script

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/scripts"
	"github.com/rs/zerolog/log"
)

const SHELL = "/bin/bash"

func Run(ctx context.Context, scripts ...string) error {
	for _, script := range scripts {
		err := runScript(ctx, script)
		if err != nil {
			return err
		}
	}
	return nil
}

func runScript(ctx context.Context, script string) error {
	scriptWithUtils := scripts.Utils + "\n" + script

	cmd := exec.CommandContext(ctx, SHELL)
	cmd.Stdin = strings.NewReader(scriptWithUtils)

	logger := log.Ctx(ctx)

	if logger != nil {
		writer := logging.Writer(logger)
		cmd.Stdout = writer
		cmd.Stderr = writer
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}

// Chroot wraps a script to execute within a chroot environment
func Chroot(path string, script string) string {
	return fmt.Sprintf(`chroot %s %s -c %s`, path, SHELL, escapeShellArg(scripts.Utils+"\n"+script))
}

// escapeShellArg escapes a string for safe use as a shell argument
func escapeShellArg(arg string) string {
	// Replace single quotes with '\'' (end quote, escaped quote, start quote)
	escaped := strings.ReplaceAll(arg, "'", "'\\''")
	return "'" + escaped + "'"
}
