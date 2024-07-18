package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/cedana/cedana/cmd"
	"github.com/containers/storage/pkg/reexec"
)

func main() {
	// Grandparent context to deal with OS interrupts
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer stop()

	// Calls the reexec init function so that storage-mountfrom is able to be called in crio rootfs
	// checkpoint. storage-mountfrom is used when the mountdata for the mount syscall is greater than
	// the page size of the os
	if reexec.Init() {
		os.Exit(1)
	}

	if err := cmd.Execute(ctx); err != nil {
		os.Exit(1)
	}
}
