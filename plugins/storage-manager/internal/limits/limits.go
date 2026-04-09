package limits

import (
	"context"
	"strings"
	"sync"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var UsedLimit int64 = 0
var Lock sync.Mutex

func CheckStorageLimit(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		if config.Global.LocalStorageLimit == 0 {
			return next(ctx, opts, resp, req)
		}

		if strings.Contains(req.GetDir(), "://") {
			return next(ctx, opts, resp, req)
		}

		Lock.Lock()
		if config.Global.LocalStorageLimit*utils.GIBIBYTE <= UsedLimit {
			Lock.Unlock()
			return nil, status.Errorf(codes.ResourceExhausted, "storage limit exceeded: used %d bytes, limit %d bytes", UsedLimit, config.Global.LocalStorageLimit*utils.GIBIBYTE)
		}
		Lock.Unlock()

		code, err = next(ctx, opts, resp, req)
		if err != nil {
			return nil, err
		}

		size := utils.SizeFromPath(req.GetDir())
		Lock.Lock()
		if UsedLimit+size >= config.Global.LocalStorageLimit*utils.GIBIBYTE {
			Lock.Unlock()
			return nil, status.Errorf(codes.ResourceExhausted, "storage limit exceeded after checkpoint: used %d bytes + size %d bytes >= limit %d bytes", UsedLimit, size, config.Global.LocalStorageLimit*utils.GIBIBYTE)
		}
		UsedLimit += size
		Lock.Unlock()

		return code, err
	}
}
