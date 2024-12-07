package io

import (
	"os"
	"os/exec"

	"github.com/containerd/go-runc"
)

type FileIO struct {
	runc.IO

	stdin  *os.File
	stdout *os.File
	stderr *os.File
}

// Implmentation for runc.IO for file I/O>
// Only stdout and stderr are set will be stdin is set to /dev/null
func NewFileIO(
	file *os.File,
) (io runc.IO) {
	return &FileIO{
		stdout: file,
		stderr: file,
	}
}

func (f *FileIO) Set(cmd *exec.Cmd) {
	cmd.Stdin = nil // /dev/null
	cmd.Stdout = f.stdout
	cmd.Stderr = f.stderr
}
