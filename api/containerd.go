package api

// Implements the task service functions for containerd

import (
	"context"

	"github.com/cedana/cedana/api/containerd"
	"github.com/cedana/cedana/api/kube"
	"github.com/cedana/cedana/api/runc"
	"github.com/cedana/cedana/api/services/task"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *service) ContainerdDump(ctx context.Context, args *task.ContainerdDumpArgs) (*task.ContainerdDumpResp, error) {
	// XXX YA: Should be free from k8s
	root := K8S_RUNC_ROOT

	pid, err := runc.GetPidByContainerId(args.ContainerID, root)
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}

	state, err := s.generateState(ctx, pid)
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	state.JobState = task.JobState_JOB_RUNNING

	err = s.containerdDump(ctx, args.Ref, args.ContainerID, state)
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}

	// TODO: Update state to add a job

	return &task.ContainerdDumpResp{
		Message:        "Dumped containerd container",
		CheckpointPath: state.CheckpointPath,
	}, nil
}

func (s *service) ContainerdRestore(ctx context.Context, args *task.ContainerdRestoreArgs) (*task.ContainerdRestoreResp, error) {
	err := s.containerdRestore(ctx, args.ImgPath, args.ContainerID)
	if err != nil {
		err = status.Error(codes.InvalidArgument, "arguments are invalid, container not found")
		return nil, err
	}
	return &task.ContainerdRestoreResp{
		Message: "Restored containerd container",
	}, nil
}

func (s *service) ContainerdQuery(ctx context.Context, args *task.ContainerdQueryArgs) (*task.ContainerdQueryResp, error) {
	var containers []*task.ContainerdContainer

	runcContainers, err := kube.StateList(args.Root)
	if err != nil {
		return nil, err
	}

	for _, c := range runcContainers {
		var container task.ContainerdContainer

		if c.Annotations[kube.CONTAINER_TYPE] == kube.CONTAINER_TYPE_CONTAINER {
			container.ContainerName = c.Annotations[kube.CONTAINER_NAME]
			container.ImageName = c.Annotations[kube.IMAGE_NAME]
			container.SandboxId = c.Annotations[kube.SANDBOX_ID]
			container.SandboxName = c.Annotations[kube.SANDBOX_NAME]
			container.SandboxUid = c.Annotations[kube.SANDBOX_UID]
			container.SandboxNamespace = c.Annotations[kube.SANDBOX_NAMESPACE]

			if c.Annotations[kube.SANDBOX_NAMESPACE] == args.Namespace || args.Namespace == "" && container.ImageName != "" {
				containers = append(containers, &container)
			}
		}
	}

	return &task.ContainerdQueryResp{
		Containers: containers,
	}, nil
}

func (s *service) ContainerdRootfsDump(ctx context.Context, args *task.ContainerdRootfsDumpArgs) (*task.ContainerdRootfsDumpResp, error) {

	containerdService, err := containerd.New(ctx, args.Address, s.logger)
	if err != nil {
		return &task.ContainerdRootfsDumpResp{}, err
	}

	ref, err := containerdService.DumpRootfs(ctx, args.ContainerID, args.ImageRef, args.Namespace)
	if err != nil {
		return &task.ContainerdRootfsDumpResp{}, err
	}

	return &task.ContainerdRootfsDumpResp{ImageRef: ref}, nil
}

func (s *service) ContainerdRootfsRestore(ctx context.Context, args *task.ContainerdRootfsRestoreArgs) (*task.ContainerdRootfsRestoreResp, error) {
	resp := &task.ContainerdRootfsRestoreResp{}

	// containerdService, err := containerd.New(ctx, args.Address, s.logger)
	// if err != nil {
	// 	return resp, err
	// }

	// if err := containerdService.RestoreRootfs(ctx, args.ContainerID, args.ImageRef, args.Namespace); err != nil {
	// 	return resp, err
	// }

	resp.ImageRef = args.ImageRef

	return resp, nil
}
