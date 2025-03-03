package filesystem

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
)

func prepareDumpDir() {
	fmt.Println("Preparing dump directory")
}

func PrepareDumpDir(next types.DumpVM) types.DumpVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (exited chan int, err error) {
		dir := req.GetDir()
		req.Dir = fmt.Sprint("file://", dir)
		return exited, nil
	}
}
