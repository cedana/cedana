package filesystem

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
)

func PrepareDumpDir(next types.DumpVM) types.DumpVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (code func() <-chan int, err error) {
		dir := req.GetDir()
		req.Dir = fmt.Sprint("file://", dir)

		return next(ctx, opts, resp, req)
	}
}

func PrepareDumpDirForRestore(next types.RestoreVM) types.RestoreVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreVMResp, req *daemon.RestoreVMReq) (code func() <-chan int, err error) {
		dir := req.GetVMSnapshotPath()
		req.VMSnapshotPath = fmt.Sprint("file://", dir)

		return next(ctx, opts, resp, req)
	}
}
