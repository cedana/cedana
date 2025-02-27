package runc

import (
	"os"
	"os/signal"

	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/utils"
	"github.com/rs/zerolog/log"

	"golang.org/x/sys/unix"
)

const signalBufferSize = 2048

// newSignalHandler returns a signal handler for processing SIGCHLD and SIGWINCH signals
// while still forwarding all other signals to the process.
// If notifySocket is present, use it to read systemd notifications from the container and
// forward them to notifySocketHost.
func NewSignalHandler(notifySocket *notifySocket) *SignalHandler {
	// ensure that we have a large buffer size so that we do not miss any signals
	// in case we are not processing them fast enough.
	s := make(chan os.Signal, signalBufferSize)
	// handle all signals for the process.
	signal.Notify(s)
	return &SignalHandler{
		signals:      s,
		notifySocket: notifySocket,
	}
}

// exit models a process exit status with the pid and
// exit status.
type exit struct {
	pid    int
	status int
}

type SignalHandler struct {
	signals      chan os.Signal
	notifySocket *notifySocket
}

// forward handles the main signal event loop forwarding, resizing, or reaping depending
// on the signal received.
func (h *SignalHandler) Forward(process *libcontainer.Process, tty *Tty, detach bool) (int, error) {
	// make sure we know the pid of our main process so that we can return
	// after it dies.
	if detach && h.notifySocket == nil {
		return 0, nil
	}

	pid1, err := process.Pid()
	if err != nil {
		return -1, err
	}

	if h.notifySocket != nil {
		if detach {
			_ = h.notifySocket.run(pid1)
			return 0, nil
		}
		_ = h.notifySocket.run(os.Getpid())
		go func() { _ = h.notifySocket.run(0) }()
	}

	// Perform the initial tty resize. Always ignore errors resizing because
	// stdout might have disappeared (due to races with when SIGHUP is sent).
	_ = tty.resize()
	// Handle and forward signals.
	for s := range h.signals {
		switch s {
		case unix.SIGWINCH:
			// Ignore errors resizing, as above.
			_ = tty.resize()
		case unix.SIGCHLD:
			exits, err := h.reap()
			if err != nil {
				log.Error().Err(err).Msg("reap failed")
			}
			for _, e := range exits {
				log.Debug().Int("pid", e.pid).Int("status", e.status).Msg("process exited")
				if e.pid == pid1 {
					// call Wait() on the process even though we already have the exit
					// status because we must ensure that any of the go specific process
					// fun such as flushing pipes are complete before we return.
					_, _ = process.Wait()
					return e.status, nil
				}
			}
		case unix.SIGURG:
			// SIGURG is used by go runtime for async preemptive
			// scheduling, so runc receives it from time to time,
			// and it should not be forwarded to the container.
			// Do nothing.
		default:
			us := s.(unix.Signal)
			log.Debug().Int("signal", int(us)).Str("name", unix.SignalName(us)).Int("pid", pid1).Msg("forwarding signal")
			if err := unix.Kill(pid1, us); err != nil {
				log.Error().Err(err).Int("signal", int(us)).Str("name", unix.SignalName(us)).Int("pid", pid1).Msg("failed to forward signal")
			}
		}
	}
	return -1, nil
}

// reap runs wait4 in a loop until we have finished processing any existing exits
// then returns all exits to the main event loop for further processing.
func (h *SignalHandler) reap() (exits []exit, err error) {
	var (
		ws  unix.WaitStatus
		rus unix.Rusage
	)
	for {
		pid, err := unix.Wait4(-1, &ws, unix.WNOHANG, &rus)
		if err != nil {
			if err == unix.ECHILD {
				return exits, nil
			}
			return nil, err
		}
		if pid <= 0 {
			return exits, nil
		}
		exits = append(exits, exit{
			pid:    pid,
			status: utils.ExitStatus(ws),
		})
	}
}
