package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/cedana/cedana/cmd"
)

const EXIT_ERR_CODE = 1

func main() {
	// Grandparent context to deal with OS interrupts
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cmd.Execute(ctx)
}
