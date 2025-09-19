package process

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
)

var Unfreeze types.Unfreeze = unfreeze

func unfreeze(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
	return nil, fmt.Errorf("unfreeze not implemented for processes")
}
