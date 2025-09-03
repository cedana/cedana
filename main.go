package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/cedana/cedana/cmd"
	"github.com/cedana/cedana/pkg/version"
)

// loaded from ldflag definitions
var Version = "dev"

func main() {
	// Grandparent context to deal with OS interrupts
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	signal.Ignore(syscall.SIGPIPE) // Avoid program termination due to broken pipes

	defer stop()
	version.PutVersion(Version)

	if err := cmd.Execute(ctx, Version); err != nil {
		os.Exit(1)
	}
}
