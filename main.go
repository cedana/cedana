package main

import (
	"context"
	"fmt"
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

	if err := run(ctx, Version); err != nil {
		fmt.Fprintf(os.Stderr, "cedana: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, version string) error {
	return cmd.Execute(ctx, version)
}
