package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/cedana/cedana/cmd"
	"github.com/containers/storage/pkg/reexec"
	"github.com/rs/zerolog"
)

// loaded from ldflag definitions
var Version = "dev"

func main() {
	logLevel := os.Getenv("CEDANA_LOGGER_LEVEL")

	logLevels := map[string]zerolog.Level{
		"debug":    zerolog.DebugLevel,
		"info":     zerolog.InfoLevel,
		"warn":     zerolog.WarnLevel,
		"error":    zerolog.ErrorLevel,
		"fatal":    zerolog.FatalLevel,
		"panic":    zerolog.PanicLevel,
		"trace":    zerolog.TraceLevel,
		"disabled": zerolog.Disabled,
	}

	level, ok := logLevels[logLevel]
	if !ok {
		// default logger level
		level = zerolog.DebugLevel
	}

	zerolog.SetGlobalLevel(level)

	// Grandparent context to deal with OS interrupts
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer stop()

	// Calls the reexec init function so that storage-mountfrom is able to be called in crio rootfs
	// checkpoint. storage-mountfrom is used when the mountdata for the mount syscall is greater than
	// the page size of the os
	if reexec.Init() {
		os.Exit(1)
	}

	if err := cmd.Execute(ctx, Version); err != nil {
		os.Exit(1)
	}
}
