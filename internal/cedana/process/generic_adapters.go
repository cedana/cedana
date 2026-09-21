package process

import (
	"context"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"syscall"

	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	OUT_FILE_PERMS os.FileMode = 0o644
	OUT_FILE_FLAGS int         = os.O_CREATE | os.O_WRONLY | os.O_APPEND | os.O_TRUNC

	// Large enough buffer so we don't miss signals while forwarding a burst of them.
	SIGNAL_BUFFER_SIZE = 2048
)

// Sets up the IO files for the handlers to simply pick up and plug in
func SetupIO[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (code func() <-chan int, err error) {
		var stdin io.Reader
		var stdout, stderr io.Writer

		id := rand.Uint32()

		if opts.Serverless {
			stdin = os.Stdin
			stdout = os.Stdout
			stderr = os.Stderr
		} else if types.Attachable(req) {
			stdin, stdout, stderr = cedana_io.NewStreamIOSlave(opts.Lifetime, opts.WG, id, types.WaitFirstMaster(req))
			defer func() {
				if err == nil {
					cedana_io.SetIOSlaveExitCode(id, code())
					cedana_io.SetIOSlavePID(id, types.PID(resp)) // Since PID should be available at this point
				}
			}()
		} else if types.Log(req) != "" {
			outFile, err := os.OpenFile(types.Log(req), OUT_FILE_FLAGS, OUT_FILE_PERMS)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to open log file: %v", err)
			}
			defer outFile.Close()
			err = os.Chown(types.Log(req), int(types.UID(req)), int(types.GID(req)))
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to change log file owner: %v", err)
			}
			stdout, stderr = outFile, outFile
		}

		opts.IO.Stdin = stdin
		opts.IO.Stdout = stdout
		opts.IO.Stderr = stderr

		return next(ctx, opts, resp, req)
	}
}

// Sets up signal forwarding to the actual process for serverless mode
func ForwardSignals[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (code func() <-chan int, err error) {
		// Only forward signals in serverless mode, as the process is running as the only child of the current process.
		if !opts.Serverless {
			log.Debug().Str("type", types.Type(req)).Msg("signal forwarding skipped because not in serverless mode")
			return next(ctx, opts, resp, req)
		}

		code, err = next(ctx, opts, resp, req)
		if err != nil {
			return nil, err
		}

		pid := int(types.PID(resp))
		if pid == 0 {
			log.Debug().Str("type", types.Type(req)).Msg("signal forwarding skipped because no PID available")
			return code, nil
		}

		// signal.Notify with no signals listed catches everything we're allowed to catch,
		// so the child sees the same signals we do.
		signals := make(chan os.Signal, SIGNAL_BUFFER_SIZE)
		signal.Notify(signals)

		exited := code()

		log.Debug().Int("PID", pid).Msg("signal forwarding starting")

		opts.WG.Go(func() {
			defer signal.Stop(signals)
			for {
				select {
				case <-exited: // stop forwarding once the exit code channel is closed
					log.Debug().Int("PID", pid).Msg("signal forwarding stopped")
					return
				case s := <-signals:
					sig, ok := s.(syscall.Signal)
					if !ok {
						continue
					}
					switch sig {
					case syscall.SIGWINCH:
						// SIGWINCH is used to notify the process of terminal resize events,
						// this is expected to be handled by the plugin of the job type
						// so we don't forward it to the process
						continue
					case syscall.SIGCHLD:
						// We reap the child ourselves in the run/restore handlers (or some plugins do it themselves),
						// so forwarding SIGCHLD would just be noise for the process.
						continue
					case syscall.SIGURG:
						// Used by the Go runtime for async preemption, never meant for the process.
						continue
					}
					log.Debug().Int("PID", pid).Stringer("signal", sig).Msg("forwarding signal")
					if err := syscall.Kill(pid, sig); err != nil {
						log.Trace().Err(err).Int("PID", pid).Stringer("signal", sig).Msg("failed to forward signal")
					}
				}
			}
		})

		return code, nil
	}
}
