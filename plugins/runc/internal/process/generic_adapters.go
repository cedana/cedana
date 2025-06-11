package process

import (
	"context"
	"fmt"
	"os"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"golang.org/x/sys/unix"
)

func SetUsChildSubReaper[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (func() <-chan int, error) {
		daemonless, _ := ctx.Value(keys.DAEMONLESS_CONTEXT_KEY).(bool)
		if daemonless {
			// For daemonless mode, we never become a subreaper as we are not managing the container.
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
