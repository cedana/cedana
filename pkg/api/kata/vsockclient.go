package kata

import (
	"context"

	task "buf.build/gen/go/cedana/task/protocolbuffers/go"
	"github.com/cedana/cedana/pkg/api/services"
)

type VSockClientInterface interface {
	KataDump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error)
	Close()
}

func NewVSockClient(vmName string, port uint32) (VSockClientInterface, error) {
	return services.NewVSockClient(vmName, port)
}
