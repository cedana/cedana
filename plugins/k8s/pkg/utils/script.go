package utils

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

func RunScript(ctx context.Context, script string) error {
	cmd := exec.CommandContext(ctx, "bash")
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
