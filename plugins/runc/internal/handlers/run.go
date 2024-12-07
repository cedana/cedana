package handlers

// Defines run (runc) handlers that ship with this plugin

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"

	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	runc_io "github.com/cedana/cedana/plugins/runc/pkg/io"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	runc_client "github.com/containerd/go-runc"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	RUNC_LOG_FILE  = "runc.log"
	RUNC_LOG_DEBUG = true

	// SIGINT works since we're not detaching, and the contianer process
	// is tied to the `runc run` command that's run by the runc client.
	// SIGKILL fails here, and causes just the `runc run` to exit, but the
	// container process is left running.
	KILL_SIGNAL = syscall.SIGINT

	waitForRunErrTimeout = 300 * time.Millisecond
)

type RuncLogMsg struct {
	Msg   string `json:"msg"`
	Level string `json:"level"`
	Time  string `json:"time"`
}

func RuncLogMsgToString(b []byte) (string, error) {
	var msg RuncLogMsg
	err := json.Unmarshal(b, &msg)
	if err != nil {
		return "", err
	}
	return msg.Msg, nil
}

func Run() types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
		opts := req.GetDetails().GetRunc()
		root := opts.GetRoot()
		id := opts.GetID()
		noPivot := opts.GetNoPivot()
		noNewKeyring := opts.GetNoNewKeyring()

		spec, ok := ctx.Value(runc_keys.SPEC_CONTEXT_KEY).(*specs.Spec)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get spec from context")
		}

		spec.Process.Terminal = false // force pass-through terminal, since we're managing it

		// Apply updated spec to the bundle
		err = runc.UpdateSpec(runc.SpecConfigFile, spec)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to apply spec: %v", err)
		}
		defer runc.RestoreSpec(runc.SpecConfigFile)

		// Attach IO if requested, otherwise log to file
		exitCode := make(chan int, 1)
		var io runc_client.IO
		if req.Attachable {
			// Use a random number, since we don't have PID yet
			id := rand.Uint32()
			io = runc_io.NewStreamIOSlave(server.Lifetime, id, exitCode)
			defer cedana_io.SetIOSlavePID(id, &resp.PID) // PID should be available then
		} else {
			logFile, ok := ctx.Value(keys.RUN_LOG_FILE_CONTEXT_KEY).(*os.File)
			if !ok {
				return nil, status.Errorf(codes.Internal, "failed to get log file from context")
			}
			io = runc_io.NewFileIO(logFile)
		}

		os.Remove(RUNC_LOG_FILE) // remove old log file

		client := runc_client.Runc{
			Root:      root,
			Log:       RUNC_LOG_FILE,
			LogFormat: runc_client.JSON,
			Debug:     RUNC_LOG_DEBUG,
		}

		version, err := client.Version(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get runc version: %v", err)
		}

		pid := make(chan int, 1)
		runErr := make(chan error, 1)
		exited = make(chan int)

		server.WG.Add(1)
		go func() {
			defer server.WG.Done()
			code, err := client.Run(server.Lifetime, id, "", &runc_client.CreateOpts{
				IO:           runc_io.WithCancelSignal(io, KILL_SIGNAL),
				NoPivot:      noPivot,
				NoNewKeyring: noNewKeyring,
				Started:      pid,
			})
			if err != nil {
				runErr <- err
				close(runErr)
			}
			log.Debug().Int("code", code).Msg("runc container exited")
			exitCode <- code
			close(exitCode)
			close(exited)
		}()

		// Wait for some time to see if there's an error
		select {
		case <-time.After(waitForRunErrTimeout):
		case err := <-runErr:
			// Capture logs from runc
			lastMsg := utils.LogFromFile(
				log.With().
					Str("runc", version.Runc).
					Str("spec_max", version.Spec).
					Str("spec_current", spec.Version).
					Logger().WithContext(ctx),
				filepath.Join(RUNC_LOG_FILE),
				zerolog.TraceLevel,
				RuncLogMsgToString,
			)

			return nil, status.Errorf(codes.Internal, "failed to run container: %v: %s", err, lastMsg)
		}

		resp.PID = uint32(<-pid)

		return exited, nil
	}
}
