package cedanarpc

import (
	"context"
)

type server struct{}

// SayHello is an implementation of the SayHello method from the definition of
// the Greeter service.
func (s *server) Checkpoint(ctx context.Context, req *CheckpointRequest) (*StateResponse, error) {
	return &StateResponse{}, nil
}
