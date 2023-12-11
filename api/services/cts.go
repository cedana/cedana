package services

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"

	"github.com/cedana/cedana/api/services/gpu"
	"github.com/cedana/cedana/api/services/task"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type services struct {
	taskService task.TaskServiceClient
	gpuService  gpu.CedanaGPUClient
}

type ServiceClient struct {
	ctx context.Context
	services
	connMu   sync.Mutex
	taskConn *grpc.ClientConn
	gpuConn  *grpc.ClientConn
}

func (s *ServiceClient) TaskService() task.TaskServiceClient {
	if s.taskService != nil {
		return s.taskService
	}
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return task.NewTaskServiceClient(s.taskConn)
}

func (s *ServiceClient) GPUService() gpu.CedanaGPUClient {
	if s.gpuService != nil {
		return s.gpuService
	}
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return gpu.NewCedanaGPUClient(s.gpuConn)
}

func NewClient(addr string, ctx context.Context) *ServiceClient {

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	taskConn, err := grpc.Dial(addr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	gpuConn, err := grpc.Dial("127.0.0.1:50051", opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	taskClient := task.NewTaskServiceClient(taskConn)
	gpuClient := gpu.NewCedanaGPUClient(gpuConn)

	client := &ServiceClient{
		services: services{taskService: taskClient, gpuService: gpuClient},
		connMu:   sync.Mutex{},
		taskConn: taskConn,
		gpuConn:  gpuConn,
		ctx:      ctx,
	}
	return client
}

func (c *ServiceClient) CheckpointTask(args *task.DumpArgs) (*task.DumpResp, error) {
	if os.Getenv("CEDANA_GPU_ENABLED") == "true" {
		gpuResp, err := c.GpuCheckpoint(&gpu.CheckpointRequest{
			Directory: args.Dir,
		})
		if err != nil {
			return nil, err
		}
		if !gpuResp.Success {
			return nil, errors.New("gpu checkpoint failed")
		}
		log.Println("gpu checkpoint success")
	}
	resp, err := c.services.taskService.Dump(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RestoreTask(args *task.RestoreArgs) (*task.RestoreResp, error) {
	resp, err := c.services.taskService.Restore(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) CheckpointContainer(args *task.ContainerDumpArgs) (*task.ContainerDumpResp, error) {
	resp, err := c.services.taskService.ContainerDump(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RestoreContainer(args *task.ContainerRestoreArgs) (*task.ContainerRestoreResp, error) {
	resp, err := c.services.taskService.ContainerRestore(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) CheckpointRunc(args *task.RuncDumpArgs) (*task.RuncDumpResp, error) {
	resp, err := c.services.taskService.RuncDump(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RuncRestore(args *task.RuncRestoreArgs) (*task.RuncRestoreResp, error) {
	resp, err := c.services.taskService.RuncRestore(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) StartTask(args *task.StartTaskArgs) (*task.StartTaskResp, error) {
	resp, err := c.services.taskService.StartTask(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) GpuCheckpoint(args *gpu.CheckpointRequest) (*gpu.CheckpointResponse, error) {
	resp, err := c.services.gpuService.Checkpoint(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) GpuRestore(args *gpu.RestoreRequest) (*gpu.RestoreResponse, error) {
	resp, err := c.services.gpuService.Restore(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) Close() {
	c.gpuConn.Close()
	c.taskConn.Close()
}
