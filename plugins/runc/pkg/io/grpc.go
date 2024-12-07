package io

import (
	"context"
	"os/exec"

	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/containerd/go-runc"
)

type StreamIOSlave struct {
	runc.IO

	stdin  *cedana_io.StreamIOReader
	stdout *cedana_io.StreamIOWriter
	stderr *cedana_io.StreamIOWriter
}

// Returns a runc.IO compatible wrapper around StreamIOSlave
func NewStreamIOSlave(
	ctx context.Context,
	pid uint32,
	exitCode chan int,
) (io runc.IO) {
	stdIn, stdOut, stdErr := cedana_io.NewStreamIOSlave(ctx, pid, exitCode)

	return &StreamIOSlave{
		stdin:  stdIn,
		stdout: stdOut,
		stderr: stdErr,
	}
}

func (s *StreamIOSlave) Set(cmd *exec.Cmd) {
	cmd.Stdin = s.stdin
	cmd.Stdout = s.stdout
	cmd.Stderr = s.stderr
}
