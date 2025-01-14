package types

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
)

type Query = func(context.Context, *daemon.QueryReq) (*daemon.QueryResp, error)
