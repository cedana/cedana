package api

// Implements the task service functions for containerd

import (
	"context"

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

	state, err := s.generateState(pid)
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

	annotations, err := kube.StateList(args.Root)
	if err != nil {
		return nil, err
	}

	for _, sandbox := range annotations {
		var container task.ContainerdContainer

		if sandbox[kube.CONTAINER_TYPE] == kube.CONTAINER_TYPE_CONTAINER {
			container.ContainerName = sandbox[kube.CONTAINER_NAME]
			container.ImageName = sandbox[kube.IMAGE_NAME]
			container.SandboxId = sandbox[kube.SANDBOX_ID]
			container.SandboxName = sandbox[kube.SANDBOX_NAME]
			container.SandboxUid = sandbox[kube.SANDBOX_UID]
			container.SandboxNamespace = sandbox[kube.SANDBOX_NAMESPACE]

			if sandbox[kube.SANDBOX_NAMESPACE] == args.Namespace || args.Namespace == "" && container.ImageName != "" {
				containers = append(containers, &container)
			}
		}
	}

	return &task.ContainerdQueryResp{
		Containers: containers,
	}, nil
}
