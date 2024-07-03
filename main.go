package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/cedana/cedana/cmd"
)

func main() {
	// Grandparent context to deal with OS interrupts
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := cmd.Execute(ctx); err != nil {
		os.Exit(1)
	}
}
