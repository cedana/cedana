package process

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"golang.org/x/sys/unix"
)

func SetUsChildSubReaper(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (func() <-chan int, error) {
		defails := req.GetDetails().GetRunc()

		if defails.GetNoSubreaper() {
			return next(ctx, opts, resp, req)
		}

		err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, uintptr(1), 0, 0, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to set child subreaper: %v\n", err)
			os.Exit(1)
		}
		return next(ctx, opts, resp, req)
	}
}
