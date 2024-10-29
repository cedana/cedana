package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/cedana/cedana/cmd"
	"github.com/cedana/cedana/internal/logger"
	"github.com/rs/zerolog/log"
)

// loaded from ldflag definitions
var Version = "dev"

func main() {
	log.Logger = logger.DefaultLogger

	// Grandparent context to deal with OS interrupts
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer stop()

	if err := cmd.Execute(ctx, Version); err != nil {
		os.Exit(1)
	}
}
