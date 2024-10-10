package api

import (
	"context"
	"encoding/json"

	"github.com/cedana/cedana/pkg/api/services/task"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *service) JobDump(ctx context.Context, args *task.JobDumpArgs) (*task.JobDumpResp, error) {
	res := &task.JobDumpResp{}

	state, err := s.getState(ctx, args.JID)
	if err != nil {
		err = status.Error(codes.NotFound, err.Error())
		return nil, err
	}

	if state.GPU && s.gpuEnabled == false {
		return nil, status.Error(codes.FailedPrecondition, "GPU support is not enabled in daemon")
	}

	// Check if normal process or container
	if state.ContainerID == "" {
		dumpResp, err := s.Dump(ctx, &task.DumpArgs{
			JID:      args.JID,
			PID:      state.PID,
			Dir:      args.Dir,
			Type:     args.Type,
			Stream:   args.Stream,
			CriuOpts: args.CriuOpts,
		})
		if err != nil {
			return nil, err
		}
		res.State = dumpResp.State
		res.DumpStats = dumpResp.DumpStats
		res.CheckpointID = dumpResp.CheckpointID
		res.UploadID = dumpResp.UploadID
		res.Message = dumpResp.Message
	} else {
		dumpResp, err := s.RuncDump(ctx, &task.RuncDumpArgs{
			Dir:         args.Dir,
			Root:        state.ContainerRoot,
			ContainerID: state.ContainerID,
			CriuOpts:    args.CriuOpts,
			Type:        args.Type,
		})
		if err != nil {
			return nil, err
		}
		res.State = dumpResp.State
		res.DumpStats = dumpResp.DumpStats
		res.CheckpointID = dumpResp.CheckpointID
		res.UploadID = dumpResp.UploadID
		res.Message = dumpResp.Message
	}

	return res, nil
}

func (s *service) JobRestore(ctx context.Context, args *task.JobRestoreArgs) (*task.JobRestoreResp, error) {
	res := &task.JobRestoreResp{}

	state, err := s.getState(ctx, args.JID)
	if err != nil {
		err = status.Error(codes.NotFound, err.Error())
		return nil, err
	}

	if state.GPU && s.gpuEnabled == false {
		return nil, status.Error(codes.FailedPrecondition, "GPU support is not enabled in daemon")
	}

	// Check if normal process or container
	if state.ContainerID == "" {
		restoreResp, err := s.Restore(ctx, &task.RestoreArgs{
			JID:      args.JID,
			Stream:   args.Stream,
			CriuOpts: args.CriuOpts,
		})
		if err != nil {
			return nil, err
		}
		res.State = restoreResp.State
		res.RestoreStats = restoreResp.RestoreStats
		res.Message = restoreResp.Message
	} else {
		opts := &task.RuncOpts{}
		opts.Detach = args.RuncOpts.Detach
		opts.ConsoleSocket = args.RuncOpts.ConsoleSocket
		opts.Bundle = args.RuncOpts.Bundle
		if opts.Bundle == "" {
			// Use saved bundle if not overridden from args
			opts.Bundle = state.ContainerBundle
		}
		opts.Root = args.RuncOpts.Root
		if opts.Root == "" {
			// Use saved root if not overridden from args
			opts.Root = state.ContainerRoot
		}
		restoreResp, err := s.RuncRestore(ctx, &task.RuncRestoreArgs{
			ContainerID: state.ContainerID,
			ImagePath:   state.CheckpointPath,
			Opts:        opts,
			Type:        args.Type,
			CriuOpts:    args.CriuOpts,
		})
		if err != nil {
			return nil, err
		}
		res.State = restoreResp.State
		res.RestoreStats = restoreResp.RestoreStats
		res.Message = restoreResp.Message
	}

	return res, nil
}

func (s *service) JobRestoreAttach(stream task.TaskService_JobRestoreAttachServer) error {
	in, err := stream.Recv()
	if err != nil {
		return err
	}
	args := in.GetJobRestoreArgs()

	state, err := s.getState(stream.Context(), args.JID)
	if err != nil {
		err = status.Error(codes.NotFound, err.Error())
		return err
	}

	if state.GPU && s.gpuEnabled == false {
		return status.Error(codes.FailedPrecondition, "GPU support is not enabled in daemon")
	}

	// Check if normal process or container
	if state.ContainerID == "" {
		_, err = s.restoreHelper(stream.Context(), &task.RestoreArgs{
			JID:      args.JID,
			Stream:   args.Stream,
			CriuOpts: args.CriuOpts,
			Type:     args.Type,
		}, stream)
	} else {
		err = status.Error(codes.Unimplemented, "restore attach for runc is not supported")
	}

	return err
}

func (s *service) JobQuery(ctx context.Context, args *task.JobQueryArgs) (*task.JobQueryResp, error) {
	res := &task.JobQueryResp{}

	if len(args.JIDs) > 0 {
		for _, jid := range args.JIDs {
			state, err := s.getState(ctx, jid)
			if err != nil {
				return nil, status.Error(codes.NotFound, "job not found")
			}
			if state != nil {
				res.Processes = append(res.Processes, state)
			}
		}
	} else {
		pidSet := make(map[int32]bool)
		for _, pid := range args.PIDs {
			pidSet[pid] = true
		}

		list, err := s.db.ListJobs(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to retrieve jobs from database")
		}
		for _, job := range list {
			state := task.ProcessState{}
			err = json.Unmarshal(job.State, &state)
			if err != nil {
				return nil, status.Error(codes.Internal, "failed to unmarshal state")
			}
			if len(pidSet) > 0 && !pidSet[state.PID] {
				continue
			}
			res.Processes = append(res.Processes, &state)
		}
	}

	return res, nil
}
