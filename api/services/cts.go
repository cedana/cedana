package services

import (
	"context"
	"log"
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
	connMu sync.Mutex
	conn   *grpc.ClientConn
}

func (s *ServiceClient) TaskService() task.TaskServiceClient {
	if s.taskService != nil {
		return s.taskService
	}
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return task.NewTaskServiceClient(s.conn)
}

func (s *ServiceClient) GPUService() gpu.CedanaGPUClient {
	if s.gpuService != nil {
		return s.gpuService
	}
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return gpu.NewCedanaGPUClient(s.conn)
}

func NewClient(addr string) *ServiceClient {

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	taskClient := task.NewTaskServiceClient(conn)
	gpuClient := gpu.NewCedanaGPUClient(conn)

	client := &ServiceClient{
		services: services{taskService: taskClient, gpuService: gpuClient},
		connMu:   sync.Mutex{},
		conn:     conn,
	}
	return client
}

func (c *ServiceClient) CheckpointTask(args *task.DumpArgs) *task.DumpResp {
	resp, err := c.services.taskService.Dump(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *ServiceClient) RestoreTask(args *task.RestoreArgs) *task.RestoreResp {
	resp, err := c.services.taskService.Restore(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *ServiceClient) CheckpointContainer(args *task.ContainerDumpArgs) *task.ContainerDumpResp {
	resp, err := c.services.taskService.ContainerDump(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *ServiceClient) ContainerRestore(args *task.ContainerRestoreArgs) *task.ContainerRestoreResp {
	resp, err := c.services.taskService.ContainerRestore(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *ServiceClient) CheckpointRunc(args *task.RuncDumpArgs) *task.RuncDumpResp {
	resp, err := c.services.taskService.RuncDump(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *ServiceClient) RuncRestore(args *task.RuncRestoreArgs) *task.RuncRestoreResp {
	resp, err := c.services.taskService.RuncRestore(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *ServiceClient) StartTask(args *task.StartTaskArgs) *task.StartTaskResp {
	resp, err := c.services.taskService.StartTask(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *ServiceClient) GpuCheckpoint(args *gpu.CheckpointRequest) *gpu.CheckpointResponse {
	resp, err := c.services.gpuService.Checkpoint(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *ServiceClient) Close() {
	c.conn.Close()
}
