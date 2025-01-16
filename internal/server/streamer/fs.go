package streamer

// Implementation of the afero.Fs interface that streams read/write operations
// using the cedana-image-streamer.

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/logging"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

type Mode int

const (
	READ_ONLY Mode = iota
	WRITE_ONLY
)

const DEFAULT_NUM_PIPES = 4

type StreamerFs struct {
	mode  Mode
	ready chan any
	afero.Fs
}

func NewStreamerFs(ctx context.Context, streamerBinary string, dir string, mode Mode) (*StreamerFs, error) {
	// Start the image streamer based on the provided mode

	args := []string{"--dir", dir, "--num-pipes", strconv.Itoa(DEFAULT_NUM_PIPES)}

	switch mode {
	case READ_ONLY:
		args = append(args, "serve")
	case WRITE_ONLY:
		args = append(args, "capture")
	}

	logger := logging.Writer("streamer", dir, zerolog.TraceLevel)

	cmd := exec.CommandContext(ctx, streamerBinary, args...)
	cmd.Stdout = logger
	cmd.Stderr = logger
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) } // AVOID SIGKILL

	err := cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start streamer: %w", err)
	}

	// Clean up once it exits
	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Trace().Err(err).Msg("streamer Wait()")
		}
		log.Debug().Int("code", cmd.ProcessState.ExitCode()).Msg("streamer exited")
	}()

	ready := make(chan any, 1)
	defer close(ready)

	time.Sleep(1 * time.Second) // TODO: use better synchronization

	return &StreamerFs{mode: mode, Fs: afero.NewBasePathFs(afero.NewOsFs(), dir), ready: ready}, nil
}
