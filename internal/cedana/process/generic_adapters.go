package process

import (
	"context"
	"io"
	"math/rand"
	"os"

	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	OUT_FILE_PERMS os.FileMode = 0o644
	OUT_FILE_FLAGS int         = os.O_CREATE | os.O_WRONLY | os.O_APPEND | os.O_TRUNC
)

// Sets up the IO files for the handlers to simply pick up and plug in
func SetupIO[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (code func() <-chan int, err error) {
		var stdin io.Reader
		var stdout, stderr io.Writer

		id := rand.Uint32()

		if opts.Serverless {
			stdin = os.Stdin
			stdout = os.Stdout
			stderr = os.Stderr
		} else if types.Attachable(req) {
			stdin, stdout, stderr = cedana_io.NewStreamIOSlave(opts.Lifetime, opts.WG, id)
			defer func() {
				if err == nil {
					cedana_io.SetIOSlaveExitCode(id, code())
					cedana_io.SetIOSlavePID(id, types.PID(resp)) // Since PID should be available at this point
				}
			}()
		} else if types.Log(req) != "" {
			outFile, err := os.OpenFile(types.Log(req), OUT_FILE_FLAGS, OUT_FILE_PERMS)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to open log file: %v", err)
			}
			defer outFile.Close()
			err = os.Chown(types.Log(req), int(types.UID(req)), int(types.GID(req)))
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to change log file owner: %v", err)
			}
			stdout, stderr = outFile, outFile
		}

		opts.IO.Stdin = stdin
		opts.IO.Stdout = stdout
		opts.IO.Stderr = stderr

		return next(ctx, opts, resp, req)
	}
}
