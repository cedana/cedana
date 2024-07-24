package services

// cts encapsulates client functions to interact with the services

import (
	"context"
	"time"

	"fmt"
	"net"
	"github.com/mdlayher/vsock"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/utils"
	"github.com/cedana/cedana/api/services/task"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

const (
	DEFAULT_PROCESS_DEADLINE    = 20 * time.Minute
	DEFAULT_CONTAINERD_DEADLINE = 10 * time.Minute
	DEFAULT_RUNC_DEADLINE       = 10 * time.Minute
)

const (
	port = 9999
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

func NewKataClient(vm string) (*ServiceClient, error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	// extract cid from the process tree on host
	cid, err := utils.ExtractCID(vm)
	if err != nil {
		return nil, err
	}

	taskConn, err := grpc.Dial(fmt.Sprintf("vsock://%d:%d", cid, port), grpc.WithInsecure(), grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
		return vsock.Dial(cid, port, nil)
	}))
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

func (c *ServiceClient) HealthCheck(ctx context.Context) (bool, error) {
	healthClient := grpc_health_v1.NewHealthClient(c.taskConn)
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()

	// Health check
	resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{
		Service: "task.TaskService",
	})

	if err != nil {
		return false, err
	}

	if resp.Status == grpc_health_v1.HealthCheckResponse_SERVING {
		return true, nil
	} else {
		return false, nil
	}
}

func (c *ServiceClient) DetailedHealthCheck(ctx context.Context, args *task.DetailedHealthCheckRequest) (*task.DetailedHealthCheckResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	resp, err := c.taskService.DetailedHealthCheck(ctx, args)
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

///////////////////////////
// Kata Service Calls //
///////////////////////////

// type of Args/Resp need to change

func (c *ServiceClient) KataDump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	resp, err := c.taskService.KataDump(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

/////////////////////////////
// Streaming Service Calls //
/////////////////////////////

// TODO YA add streaming calls (move it from server.go to here)
