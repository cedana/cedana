package process

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"golang.org/x/sys/unix"
)

func SetUsChildSubReaperForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (func() <-chan int, error) {
		details := req.GetDetails().GetSlurm()

		if details.GetNoSubreaper() {
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
