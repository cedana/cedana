package services

// cts encapsulates client functions to interact with the services

import (
	"context"
	"time"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services/task"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	DEFAULT_PROCESS_DEADLINE    = 20 * time.Minute
	DEFAULT_CONTAINERD_DEADLINE = 10 * time.Minute
	DEFAULT_RUNC_DEADLINE       = 10 * time.Minute
)

type ServiceClient struct {
	taskService task.TaskServiceClient
	taskConn    *grpc.ClientConn
}

func NewClient() (*ServiceClient, error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	taskConn, err := grpc.Dial(api.ADDRESS, opts...)
	if err != nil {
		return nil, err
	}

	taskClient := task.NewTaskServiceClient(taskConn)

	client := &ServiceClient{
		taskService: taskClient,
		taskConn:    taskConn,
	}
	return client, err
}

func (c *ServiceClient) Close() {
	c.taskConn.Close()
}

/////////////////////////////
//      Health Check       //
/////////////////////////////

func (c *ServiceClient) DetailedHealthCheck(ctx context.Context) (*task.DetailedHealthCheckResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	req := &task.DetailedHealthCheckRequest{}
	resp, err := c.taskService.DetailedHealthCheck(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

///////////////////////////
// Process Service Calls //
///////////////////////////

func (c *ServiceClient) Start(ctx context.Context, args *task.StartArgs) (*task.StartResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	resp, err := c.taskService.Start(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) Dump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	// TODO NR - timeouts here need to be fixed
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	resp, err := c.taskService.Dump(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) Restore(ctx context.Context, args *task.RestoreArgs) (*task.RestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	resp, err := c.taskService.Restore(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) Query(ctx context.Context, args *task.QueryArgs) (*task.QueryResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	resp, err := c.taskService.Query(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

//////////////////////////////
////// CRIO Rootfs Dump //////
//////////////////////////////

func (c *ServiceClient) CRIORootfsDump(ctx context.Context, args *task.CRIORootfsDumpArgs) (*task.CRIORootfsDumpResp, error) {
	// TODO NR - timeouts here need to be fixed
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	resp, err := c.taskService.CRIORootfsDump(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) CRIOImagePush(ctx context.Context, args *task.CRIOImagePushArgs) (*task.CRIOImagePushResp, error) {
	// TODO NR - timeouts here need to be fixed
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	resp, err := c.taskService.CRIOImagePush(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

//////////////////////////////
// Containerd Service Calls //
//////////////////////////////

func (c *ServiceClient) ContainerdDump(ctx context.Context, args *task.ContainerdDumpArgs) (*task.ContainerdDumpResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_CONTAINERD_DEADLINE)
	defer cancel()
	resp, err := c.taskService.ContainerdDump(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) ContainerdRestore(ctx context.Context, args *task.ContainerdRestoreArgs) (*task.ContainerdRestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_CONTAINERD_DEADLINE)
	defer cancel()
	resp, err := c.taskService.ContainerdRestore(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) ContainerdQuery(ctx context.Context, args *task.ContainerdQueryArgs) (*task.ContainerdQueryResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_CONTAINERD_DEADLINE)
	defer cancel()
	resp, err := c.taskService.ContainerdQuery(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) ContainerdRootfsDump(ctx context.Context, args *task.ContainerdRootfsDumpArgs) (*task.ContainerdRootfsDumpResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_CONTAINERD_DEADLINE)
	defer cancel()
	resp, err := c.taskService.ContainerdRootfsDump(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) ContainerdRootfsRestore(ctx context.Context, args *task.ContainerdRootfsRestoreArgs) (*task.ContainerdRootfsRestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_CONTAINERD_DEADLINE)
	defer cancel()
	resp, err := c.taskService.ContainerdRootfsRestore(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

////////////////////////
// Runc Service Calls //
////////////////////////

func (c *ServiceClient) RuncDump(ctx context.Context, args *task.RuncDumpArgs) (*task.RuncDumpResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_RUNC_DEADLINE)
	defer cancel()
	resp, err := c.taskService.RuncDump(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RuncRestore(ctx context.Context, args *task.RuncRestoreArgs) (*task.RuncRestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_RUNC_DEADLINE)
	defer cancel()
	resp, err := c.taskService.RuncRestore(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RuncQuery(ctx context.Context, args *task.RuncQueryArgs) (*task.RuncQueryResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_RUNC_DEADLINE)
	defer cancel()
	resp, err := c.taskService.RuncQuery(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

/////////////////////////////
// Streaming Service Calls //
/////////////////////////////

// TODO YA add streaming calls (move it from server.go to here)
