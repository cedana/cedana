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

type CheckpointTaskService struct {
	ctx     context.Context
	client  task.TaskServiceClient
	conn    *grpc.ClientConn // Keep a reference to the connection
	address string
}

func NewCheckpointTaskService(addr string) *CheckpointTaskService {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	client := task.NewTaskServiceClient(conn)

	ctx := context.Background()

	return &CheckpointTaskService{
		client:  client,
		conn:    conn, // Keep a reference to the connection
		address: addr,
		ctx:     ctx,
	}
}

func (c *CheckpointTaskService) CheckpointTask(args *task.DumpArgs) *task.DumpResp {
	resp, err := c.client.Dump(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) RestoreTask(args *task.RestoreArgs) *task.RestoreResp {
	resp, err := c.client.Restore(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) CheckpointContainer(args *task.ContainerDumpArgs) *task.ContainerDumpResp {
	resp, err := c.client.ContainerDump(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) ContainerRestore(args *task.ContainerRestoreArgs) *task.ContainerRestoreResp {
	resp, err := c.client.ContainerRestore(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) CheckpointRunc(args *task.RuncDumpArgs) *task.RuncDumpResp {
	resp, err := c.client.RuncDump(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) RuncRestore(args *task.RuncRestoreArgs) *task.RuncRestoreResp {
	resp, err := c.client.RuncRestore(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) StartTask(args *task.StartTaskArgs) *task.StartTaskResp {
	resp, err := c.client.StartTask(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) Close() {
	c.conn.Close()
}

type GPUCheckpointTaskService struct {
	ctx     context.Context
	client  gpu.CedanaGPUClient
	conn    *grpc.ClientConn // Keep a reference to the connection
	address string
}

func NewGpuCheckpointTaskService(addr string) *GPUCheckpointTaskService {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	client := gpu.NewCedanaGPUClient(conn)

	ctx := context.Background()

	return &GPUCheckpointTaskService{
		client:  client,
		conn:    conn, // Keep a reference to the connection
		address: addr,
		ctx:     ctx,
	}
}
