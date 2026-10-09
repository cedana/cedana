package cedana

import (
	"context"
	"errors"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/upload"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// WaitUpload blocks until the background upload of an asynchronous dump has ended.
// That the upload failed is not an error of this call, it is reported in the response.
func (s *Server) WaitUpload(ctx context.Context, req *daemon.WaitUploadReq) (*daemon.WaitUploadResp, error) {
	if req.GetPath() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "path must be provided")
	}

	result, err := s.uploads.Wait(ctx, req.GetPath())
	if errors.Is(err, upload.ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "no upload found for path %s", req.GetPath())
	}
	if err != nil {
		return nil, status.FromContextError(err).Err()
	}

	resp := &daemon.WaitUploadResp{Checksum: result.Checksum}
	if result.Err != nil {
		resp.Error = result.Err.Error()
	}

	return resp, nil
}
