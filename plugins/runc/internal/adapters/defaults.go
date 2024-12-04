package adapters

// This file contains all the adapters that fill in missing request details
// with defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/protobuf/proto"
)

const defaultRoot = "/run/runc"

////////////////////////
//// Start Adapters ////
////////////////////////

func FillMissingStartDefaults(next types.Start) types.Start {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}
		if req.GetDetails().GetRuncStart() == nil {
			req.Details.RuncStart = &runc.StartDetails{}
		}
		if req.GetDetails().GetRuncStart().GetRoot() == "" {
			req.Details.RuncStart.Root = defaultRoot
		}
		if req.GetDetails().GetRuncStart().GetID() == "" {
			req.Details.RuncStart.ID = req.JID
		}
		return next(ctx, server, resp, req)
	}
}

///////////////////////
//// Dump Adapters ////
///////////////////////

func FillMissingDumpDefaults(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}

		if req.GetDetails().GetRunc() == nil {
			req.Details.Runc = &runc.Details{}
		}

		if req.GetDetails().GetRunc().GetRoot() == "" {
			req.Details.Runc.Root = defaultRoot
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		req.Criu.OrphanPtsMaster = proto.Bool(true)

		return next(ctx, server, nfy, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////
