package handlers

// Defines run (runc) handlers that ship with this plugin

import (
	"context"
	"math/rand/v2"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/specconv"
	"github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Run() types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
		opts := req.GetDetails().GetRuncRun()
		root := opts.GetRoot()
		id := opts.GetID()
		noPivot := opts.GetNoPivot()
		noNewKeyring := opts.GetNoNewKeyring()

		spec, ok := ctx.Value(runc_keys.SPEC_CONTEXT_KEY).(*specs.Spec)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get spec from context")
		}

		spec.Process.Terminal = false // use pass-through terminal

		config, err := specconv.CreateLibcontainerConfig(&specconv.CreateOpts{
			CgroupName:   id,
			NoPivotRoot:  noPivot,
			NoNewKeyring: noNewKeyring,
			Spec:         spec,
		})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create libcontainer config: %v", err)
		}

		container, err := libcontainer.Create(root, id, config)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create container: %v", err)
		}

		r := &runc.Runner{
			EnableSubreaper: true,
			ShouldDestroy:   true,
			Container:       container,
			Action:          runc.CT_ACT_RUN,
			Init:            true,
			Detach:          false, // required to link stdin/stdout/stderr
		}

		// Attach IO if requested, otherwise log to file
		exitCode := make(chan int, 1)
		if req.Attachable {
			// Use a random number, since we don't have PID yet
			id := rand.Uint32()
			stdIn, stdOut, stdErr := utils.NewStreamIOSlave(server.Lifetime, id, exitCode)
			defer utils.SetIOSlavePID(id, &resp.PID) // PID should be available then
			r.Stdin = stdIn
			r.Stdout = stdOut
			r.Stderr = stdErr
		} else {
			_, ok := ctx.Value(keys.RUN_LOG_FILE_CONTEXT_KEY).(*os.File)
			if !ok {
				return nil, status.Errorf(codes.Internal, "failed to get log file from context")
			}
			r.Stdin = os.Stdin
			r.Stdout = os.Stdout
			r.Stderr = os.Stderr
		}

		_, err = r.Run(spec.Process)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to run container: %v", err)
		}

		return nil, nil
	}
}
