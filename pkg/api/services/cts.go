package services

// cts encapsulates client functions to interact with the services

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/mdlayher/vsock"

	taskgrpc "buf.build/gen/go/cedana/task/grpc/go/_gogrpc"
	task "buf.build/gen/go/cedana/task/protocolbuffers/go"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	DEFAULT_PROCESS_DEADLINE    = 20 * time.Minute
	DEFAULT_CONTAINERD_DEADLINE = 10 * time.Minute
	DEFAULT_RUNC_DEADLINE       = 10 * time.Minute
	DEFAULT_HOST                = "0.0.0.0"
)

type ServiceClient struct {
	taskService taskgrpc.TaskServiceClient
	taskConn    *grpc.ClientConn
}

func NewClient(port uint32) (*ServiceClient, error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	address := fmt.Sprintf("%s:%d", DEFAULT_HOST, port)
	taskConn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, err
	}

	taskClient := taskgrpc.NewTaskServiceClient(taskConn)

	client := &ServiceClient{
		taskService: taskClient,
		taskConn:    taskConn,
	}
	return client, err
}

func NewVSockClient(vm string, port uint32) (*ServiceClient, error) {
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

	taskClient := taskgrpc.NewTaskServiceClient(taskConn)

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

func (c *ServiceClient) StartAttach(ctx context.Context, args *task.AttachArgs) (taskgrpc.TaskService_StartAttachClient, error) {
	opts := getDefaultCallOptions()
	stream, err := c.taskService.StartAttach(ctx, opts...)
	if err != nil {
		return nil, err
	}
	// Send the first start request
	if err := stream.Send(args); err != nil {
		return nil, err
	}
	return stream, nil
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

func (c *ServiceClient) RestoreAttach(ctx context.Context, args *task.AttachArgs) (taskgrpc.TaskService_RestoreAttachClient, error) {
	opts := getDefaultCallOptions()
	stream, err := c.taskService.RestoreAttach(ctx, opts...)
	if err != nil {
		return nil, err
	}
	// Send the first restore request
	if err := stream.Send(args); err != nil {
		return nil, err
	}
	return stream, nil
}

func (c *ServiceClient) Manage(ctx context.Context, args *task.ManageArgs) (*task.ManageResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.Manage(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

///////////////////////////
//// Job Service Calls ////
///////////////////////////

func (c *ServiceClient) JobDump(ctx context.Context, args *task.JobDumpArgs) (*task.JobDumpResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.JobDump(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) JobRestore(ctx context.Context, args *task.JobRestoreArgs) (*task.JobRestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.JobRestore(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) JobRestoreAttach(ctx context.Context, args *task.AttachArgs) (taskgrpc.TaskService_RestoreAttachClient, error) {
	opts := getDefaultCallOptions()
	stream, err := c.taskService.JobRestoreAttach(ctx, opts...)
	if err != nil {
		return nil, err
	}
	// Send the first restore request
	if err := stream.Send(args); err != nil {
		return nil, err
	}
	return stream, nil
}

func (c *ServiceClient) JobQuery(ctx context.Context, args *task.JobQueryArgs) (*task.JobQueryResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.JobQuery(ctx, args, opts...)
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

func (c *ServiceClient) RuncManage(ctx context.Context, args *task.RuncManageArgs) (*task.RuncManageResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_RUNC_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.RuncManage(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

///////////////////////////
// Kata Service Calls //
///////////////////////////

func (c *ServiceClient) HostKataDump(ctx context.Context, args *task.HostDumpKataArgs) (*task.HostDumpKataResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	resp, err := c.taskService.HostKataDump(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) KataDump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	resp, err := c.taskService.KataDump(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) KataRestore(ctx context.Context, args *task.RestoreArgs) (*task.RestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	resp, err := c.taskService.KataRestore(ctx, args)
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

//////////////////////
///      ASR       ///
//////////////////////

func (c *ServiceClient) GetContainerInfo(ctx context.Context, args *task.ContainerInfoRequest) (*task.ContainersInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.GetContainerInfo(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

////////////////////
//    JobQueue    //
////////////////////

func (c *ServiceClient) QueueCheckpoint(ctx context.Context, args *task.QueueJobCheckpointRequest) (*wrapperspb.BoolValue, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.QueueCheckpoint(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) QueueRestore(ctx context.Context, args *task.QueueJobRestoreRequest) (*wrapperspb.BoolValue, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.QueueRestore(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) QueueJobStatus(ctx context.Context, args *task.QueueJobID) (*task.QueueJobStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_PROCESS_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.taskService.JobStatus(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

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
