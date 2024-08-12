package services

// cts encapsulates client functions to interact with the services

import (
	"context"
	"time"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services/task"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
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

func (c *ServiceClient) HealthCheck(ctx context.Context) (bool, error) {
	healthClient := grpc_health_v1.NewHealthClient(c.taskConn)
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()

	opts := getDefaultCallOptions()

	// Health check
	resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{
		Service: "task.TaskService",
	}, opts...)
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
	opts := getDefaultCallOptions()
	resp, err := c.taskService.DetailedHealthCheck(ctx, args, opts...)
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
	opts := getDefaultCallOptions()
	resp, err := c.taskService.Start(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) Dump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	// TODO NR - timeouts here need to be fixed
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.Dump(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) Restore(ctx context.Context, args *task.RestoreArgs) (*task.RestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.Restore(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) Query(ctx context.Context, args *task.QueryArgs) (*task.QueryResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.Query(ctx, args, opts...)
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
	opts := getDefaultCallOptions()
	resp, err := c.taskService.CRIORootfsDump(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) CRIOImagePush(ctx context.Context, args *task.CRIOImagePushArgs) (*task.CRIOImagePushResp, error) {
	// TODO NR - timeouts here need to be fixed
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.CRIOImagePush(ctx, args, opts...)
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
	opts := getDefaultCallOptions()
	resp, err := c.taskService.ContainerdDump(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) ContainerdRestore(ctx context.Context, args *task.ContainerdRestoreArgs) (*task.ContainerdRestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_CONTAINERD_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.ContainerdRestore(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) ContainerdQuery(ctx context.Context, args *task.ContainerdQueryArgs) (*task.ContainerdQueryResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_CONTAINERD_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.ContainerdQuery(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) ContainerdRootfsDump(ctx context.Context, args *task.ContainerdRootfsDumpArgs) (*task.ContainerdRootfsDumpResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_CONTAINERD_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.ContainerdRootfsDump(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) ContainerdRootfsRestore(ctx context.Context, args *task.ContainerdRootfsRestoreArgs) (*task.ContainerdRootfsRestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_CONTAINERD_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.ContainerdRootfsRestore(ctx, args, opts...)
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
	opts := getDefaultCallOptions()
	resp, err := c.taskService.RuncDump(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RuncRestore(ctx context.Context, args *task.RuncRestoreArgs) (*task.RuncRestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_RUNC_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.RuncRestore(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RuncQuery(ctx context.Context, args *task.RuncQueryArgs) (*task.RuncQueryResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_RUNC_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.RuncQuery(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

////////////////////////////
/// Config Service Calls ///
////////////////////////////

func (c *ServiceClient) GetConfig(ctx context.Context, args *task.GetConfigRequest) (*task.GetConfigResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.GetConfig(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

/////////////////////////////
// Streaming Service Calls //
/////////////////////////////

// TODO YA add streaming calls (move it from server.go to here)

///////////////////
//    Helpers    //
///////////////////

func getDefaultCallOptions() []grpc.CallOption {
	opts := []grpc.CallOption{}
	if viper.GetBool("cli.wait_for_ready") {
		opts = append(opts, grpc.WaitForReady(true))
	}
	return opts
}
