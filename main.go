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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if reexec.Init() {
		os.Exit(1)
	}

	if err := cmd.Execute(ctx); err != nil {
		os.Exit(1)
	}
}
