package process

import (
	"context"
	"fmt"
	"os"

	"github.com/cedana/cedana/pkg/types"
	"golang.org/x/sys/unix"
)

func SetUsChildSubReaper[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (func() <-chan int, error) {
		err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, uintptr(1), 0, 0, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to set child subreaper: %v\n", err)
			os.Exit(1)
		}
		return next(ctx, opts, resp, req)
	}
}
