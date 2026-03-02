package script

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cedana/cedana/pkg/logging"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const SHELL = "/bin/bash"

var sourceScript string

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
	script = removeShebang(script)

	script = sourceScript + "\n" + script

	cmd := exec.CommandContext(ctx, SHELL)
	cmd.Stdin = strings.NewReader(script)

	logger := log.Ctx(ctx)

	if logger.GetLevel() == zerolog.Disabled {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		loggerErr := logger.Level(zerolog.WarnLevel)
		writer := logging.Writer(logger)
		writerErr := logging.Writer(&loggerErr)
		cmd.Stdout = writer
		cmd.Stderr = writerErr
	}

	return cmd.Run()
}

func Source(scripts ...string) {
	for _, script := range scripts {
		sourceScript += script + "\n"
	}
}

// Chroot wraps a script to execute within a chroot environment
func Chroot(path string, script string) string {
	script = removeShebang(script)
	return fmt.Sprintf(`chroot %s %s -c %s`, path, SHELL, escapeShellArg(sourceScript+"\n"+script))
}

// removeShebang removes the shebang line from a script if present
func removeShebang(script string) string {
	lines := strings.SplitN(script, "\n", 2)
	if len(lines) > 0 && strings.HasPrefix(lines[0], "#!") {
		if len(lines) > 1 {
			return lines[1]
		}
		return ""
	}
	return script
}

// escapeShellArg escapes a string for safe use as a shell argument
func escapeShellArg(arg string) string {
	// Replace single quotes with '\'' (end quote, escaped quote, start quote)
	escaped := strings.ReplaceAll(arg, "'", "'\\''")
	return "'" + escaped + "'"
}
