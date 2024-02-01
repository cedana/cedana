package services

// cts encapsulates functions to interact with the running grpc daemon

import (
	"context"
	"log"
	"time"

	"github.com/cedana/cedana/api/services/task"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ServiceClient struct {
	taskService task.TaskServiceClient
	taskConn    *grpc.ClientConn
}

func NewClient(addr string) *ServiceClient {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	taskConn, err := grpc.Dial(addr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	taskClient := task.NewTaskServiceClient(taskConn)

	client := &ServiceClient{
		taskService: taskClient,
		taskConn:    taskConn,
	}
	return client
}

func (c *ServiceClient) GetRuncIdByName(args *task.CtrByNameArgs) (*task.CtrByNameResp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	resp, err := c.taskService.GetRuncContainerByName(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) CheckpointTask(args *task.DumpArgs) (*task.DumpResp, error) {
	// TODO NR - timeouts here need to be fixed
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	resp, err := c.taskService.Dump(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RestoreTask(args *task.RestoreArgs) (*task.RestoreResp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	resp, err := c.taskService.Restore(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) CheckpointContainer(args *task.ContainerDumpArgs) (*task.ContainerDumpResp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	resp, err := c.taskService.ContainerDump(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RestoreContainer(args *task.ContainerRestoreArgs) (*task.ContainerRestoreResp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	resp, err := c.taskService.ContainerRestore(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) CheckpointRunc(args *task.RuncDumpArgs) (*task.RuncDumpResp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	resp, err := c.taskService.RuncDump(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) RuncRestore(args *task.RuncRestoreArgs) (*task.RuncRestoreResp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	resp, err := c.taskService.RuncRestore(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) StartTask(args *task.StartTaskArgs) (*task.StartTaskResp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	resp, err := c.taskService.StartTask(ctx, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ServiceClient) Close() {
	c.taskConn.Close()
}
