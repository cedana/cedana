package criu

// Generic adapters for CRIU

import (
	"context"
	"os/exec"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Creates a new instance of CRIU and passes it to the server opts
func New[REQ, RESP any](manager plugins.Manager) types.Adapter[types.Handler[REQ, RESP]] {
	return func(next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
		return func(ctx context.Context, server types.ServerOpts, resp *RESP, req *REQ) (chan int, error) {
			criuInstance := criu.MakeCriu()

			// Check if CRIU plugin is installed, then use that binary
			var p *plugins.Plugin
			if p = manager.Get("criu"); p.Status != plugins.Installed {
				// Set custom path if specified in config, as a fallback
				if custom_path := config.Global.CRIU.BinaryPath; custom_path != "" {
					criuInstance.SetCriuPath(custom_path)
				} else if path, err := exec.LookPath("criu"); err == nil {
					criuInstance.SetCriuPath(path)
				} else {
					return nil, status.Error(codes.FailedPrecondition, "Please install CRIU plugin, or specify path in config or env var.")
				}
			} else {
				criuInstance.SetCriuPath(p.Binaries[0])
			}

			server.CRIU = criuInstance
			server.CRIUCallback = &criu.NotifyCallbackMulti{}

			return next(ctx, server, resp, req)
		}
	}
}
