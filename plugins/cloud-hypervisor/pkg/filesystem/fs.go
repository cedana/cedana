package filesystem

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
)

func PrepareDumpDir(next types.DumpVM) types.DumpVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (exited chan int, err error) {
		dir := req.GetDir()
		req.Dir = fmt.Sprint("file://", dir)

		exited, err = next(ctx, opts, resp, req)
		if err != nil {
			return nil, err
		}

		return exited, nil
	}
}

func PrepareDumpDirForRestore(next types.RestoreVM) types.RestoreVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreVMResp, req *daemon.RestoreVMReq) (exited chan int, err error) {
		dir := req.GetVMSnapshotPath()
		req.VMSnapshotPath = fmt.Sprint("file://", dir)

		exited, err = next(ctx, opts, resp, req)
		if err != nil {
			return nil, err
		}

		return exited, nil
	}
}
