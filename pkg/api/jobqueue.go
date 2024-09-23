package api

import (
	"context"

	"github.com/cedana/cedana/pkg/api/services/task"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func (s *service) QueueCheckpoint(ctx context.Context, args *task.QueueJobCheckpointRequest) (*wrapperspb.BoolValue, error) {
	err := s.jobService.Checkpoint(args)
	return &wrapperspb.BoolValue{Value: true}, err
}

func (s *service) QueueRestore(ctx context.Context, args *task.QueueJobRestoreRequest) (*wrapperspb.BoolValue, error) {
	err := s.jobService.Restore(args)
	return &wrapperspb.BoolValue{Value: true}, err
}

func (s *service) JobStatus(ctx context.Context, args *task.QueueJobID) (*task.QueueJobStatus, error) {
	return nil, nil
}
