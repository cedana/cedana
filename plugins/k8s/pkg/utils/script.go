package utils

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
)

func RunScript(ctx context.Context, script string, logger ...io.Writer) error {
	cmd := exec.CommandContext(ctx, "bash")
	cmd.Stdin = strings.NewReader(script)

	if len(logger) > 0 {
		cmd.Stdout = io.MultiWriter(logger...)
		cmd.Stderr = io.MultiWriter(logger...)
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}
