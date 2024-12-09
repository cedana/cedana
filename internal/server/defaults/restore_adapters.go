package defaults

import (
	"context"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
)

// Adapter that fills missing info from the request using config defaults
func FillMissingRestoreDefaults(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		// Nothing to do, yet

		ctx = context.WithValue(ctx, keys.EXTRA_FILES_CONTEXT_KEY, []*os.File{})

		return next(ctx, server, nfy, resp, req)
	}
}
