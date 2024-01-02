package services

import (
	"context"
	"log"
	"sync"

	"github.com/cedana/cedana/api/services/task"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ServiceClient struct {
	ctx         context.Context
	taskService task.TaskServiceClient
	connMu      sync.Mutex
	taskConn    *grpc.ClientConn
}

func (s *ServiceClient) TaskService() task.TaskServiceClient {
	if s.taskService != nil {
		return s.taskService
	}
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return task.NewTaskServiceClient(s.taskConn)
}

func NewClient(addr string, ctx context.Context) *ServiceClient {

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	taskConn, err := grpc.Dial(addr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	taskClient := task.NewTaskServiceClient(taskConn)

	client := &ServiceClient{
		taskService: taskClient,
		connMu:      sync.Mutex{},
		taskConn:    taskConn,
		ctx:         ctx,
	}
	return client
}

func (c *ServiceClient) CheckpointTask(args *task.DumpArgs) (*task.DumpResp, error) {
	resp, err := c.taskService.Dump(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RestoreTask(args *task.RestoreArgs) (*task.RestoreResp, error) {
	resp, err := c.taskService.Restore(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) CheckpointContainer(args *task.ContainerDumpArgs) (*task.ContainerDumpResp, error) {
	resp, err := c.taskService.ContainerDump(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RestoreContainer(args *task.ContainerRestoreArgs) (*task.ContainerRestoreResp, error) {
	resp, err := c.taskService.ContainerRestore(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) CheckpointRunc(args *task.RuncDumpArgs) (*task.RuncDumpResp, error) {
	resp, err := c.taskService.RuncDump(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RuncRestore(args *task.RuncRestoreArgs) (*task.RuncRestoreResp, error) {
	resp, err := c.taskService.RuncRestore(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) StartTask(args *task.StartTaskArgs) (*task.StartTaskResp, error) {
	resp, err := c.taskService.StartTask(c.ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) Close() {
	c.taskConn.Close()
}
