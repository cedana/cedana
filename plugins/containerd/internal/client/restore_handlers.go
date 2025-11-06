package client

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
)

var Restore types.Restore = restore

// For containerd, we simply invoke the Run handler as the container has been set up
// by now to use an appropriate Cedana containerd runtime, which will simply do a
// low-level runtime restore instead of a run.
func restore(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
	runResp := &daemon.RunResp{
		PID:      resp.PID,
		Messages: resp.Messages,
	}

	code, err = Run(ctx, opts, runResp, &daemon.RunReq{
		Type:       req.GetType(),
		JID:        req.GetDetails().GetJID(),
		Details:    req.GetDetails(),
		PidFile:    req.GetPidFile(),
		Log:        req.GetLog(),
		Attachable: req.GetAttachable(),
		UID:        req.GetUID(),
		GID:        req.GetGID(),
		Groups:     req.GetGroups(),
		Env:        req.GetEnv(),
	})

	resp.PID = runResp.PID
	resp.Messages = runResp.Messages

	return code, err
}
