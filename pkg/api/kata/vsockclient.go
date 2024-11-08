package kata

import (
	"context"

	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/api/services/task"
)

type VSockClientInterface interface {
	KataDump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error)
	Close()
}

func NewVSockClient(vmName string, port uint32) (VSockClientInterface, error) {
	return services.NewVSockClient(vmName, port)
}
