package server

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/server/criu"
	"github.com/cedana/cedana/pkg/types"
)

const CRIU_EXPERIMENTAL_CHECKS = true

func (s *Server) HealthCheck(ctx context.Context, req *daemon.HealthCheckReq) (*daemon.HealthCheckResp, error) {
	checklist := types.Checklist{
		{
			Name: "criu",
			List: []types.Check{
				criu.CheckVersion(s.plugins),
				criu.CheckFeatures(s.plugins, CRIU_EXPERIMENTAL_CHECKS),
			},
		},

		// TODO: add more kernel feature checks
	}

	if req.Full {
		checklist = append(checklist, s.pluginChecklist()...)
	}

	results := checklist.Run(ctx)

	return &daemon.HealthCheckResp{Results: results}, nil
}

func (s *Server) pluginChecklist() types.Checklist {
	checklist := []types.Checks{}

	// Add a GPU helth check if plugin is installed
	if s.plugins.IsInstalled("gpu") {
		checklist = append(checklist, s.gpus.Checks())
	}

	// Add health checks from other plugins
	return checklist
}
