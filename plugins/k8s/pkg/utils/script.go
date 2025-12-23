package utils

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
)

func RunScript(ctx context.Context, script string, logger ...io.Writer) error {
	stdouts := []io.Writer{os.Stdout}
	stdouts = append(stdouts, logger...)
	stderrs := []io.Writer{os.Stderr}
	stderrs = append(stderrs, logger...)

	cmd := exec.CommandContext(ctx, "bash")
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = io.MultiWriter(stdouts...)
	cmd.Stderr = io.MultiWriter(stderrs...)
	return cmd.Run()
}
