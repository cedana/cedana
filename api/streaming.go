package api

import (
	"context"
	"fmt"
	"time"

	"github.com/cedana/cedana/api/services/task"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Implements the task service functions for streaming

const (
	PROCESS_STREAMING_RATE_SECONDS = 30
	LOG_STREAMING_RATE_SECONDS     = 10
)

// This is for the orchestrator
func (s *service) LogStreaming(stream task.TaskService_LogStreamingServer) error {
	limiter := rate.NewLimiter(rate.Every(LOG_STREAMING_RATE_SECONDS), 5)
	buf := make([]byte, 4096)

	for {
		select {
		case <-stream.Context().Done():
			return nil // Client disconnected
		default:
			n, err := s.logFile.Read(buf)
			if err != nil {
				break
			}
			if limiter.Allow() {
				// TODO BS Needs implementation
				response := &task.LogStreamingArgs{
					Timestamp: time.Now().Local().Format(time.RFC3339),
					Source:    SERVER_LOG_PATH,
					Level:     "INFO",
					Msg:       string(buf[:n]),
				}
				if err := stream.Send(response); err != nil {
					return err
				}
			}
		}
	}
}

// This is for the orchestrator
func (s *service) ProcessStateStreaming(args *task.ProcessStateStreamingArgs, stream task.TaskService_ProcessStateStreamingServer) error {
	// Early return if no JID
	jid := args.JID
	_, err := s.getState(jid)
	if err != nil {
		return status.Errorf(codes.NotFound, "job %s not found", jid)
	}

	ctx, cancel := context.WithCancelCause(stream.Context())

	go func() {
		ticker := time.NewTicker(time.Duration(PROCESS_STREAMING_RATE_SECONDS) * time.Second)
		for range ticker.C {
			state, err := s.getState(jid)
			if err != nil {
				cancel(fmt.Errorf("error getting state: ", err))
				return
			}

			err = stream.Send(state)
			if err != nil {
				cancel(fmt.Errorf("error sending state: ", err))
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return status.Errorf(codes.Canceled, "%v", ctx.Err())
	case <-stream.Context().Done():
		cancel(fmt.Errorf("streaming cancelled"))
		return status.Errorf(codes.Canceled, "%v", stream.Context().Err())
	}
}
