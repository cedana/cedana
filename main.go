package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/cedana/cedana/cmd"
	"github.com/cedana/cedana/internal/logger"
	"github.com/rs/zerolog/log"
)

// loaded from ldflag definitions
var Version = "dev"

func main() {
	// Grandparent context to deal with OS interrupts
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	defer stop()

	log.Logger = logger.DefaultLogger

	if err := cmd.Execute(ctx, Version); err != nil {
		os.Exit(1)
	}
}
