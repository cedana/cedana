package io

import (
	"os"
	"os/exec"

	"github.com/containerd/go-runc"
)

type CancelIO struct {
	runc.IO
	signal os.Signal
}

// A wrapper for any runc.IO that signals the process
// with a custom signal when the context is cancelled.
func WithCancelSignal(io runc.IO, signal os.Signal) (cancelIO runc.IO) {
	return &CancelIO{io, signal}
}

func (c *CancelIO) Set(cmd *exec.Cmd) {
	c.IO.Set(cmd)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(c.signal)
	}
}
