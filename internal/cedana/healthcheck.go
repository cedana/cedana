package cedana

import (
	"context"
	"slices"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana/criu"
	"github.com/cedana/cedana/internal/cedana/streamer"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
)

const CRIU_EXPERIMENTAL_CHECKS = false

func (s *Server) HealthCheck(ctx context.Context, req *daemon.HealthCheckReq) (*daemon.HealthCheckResp, error) {
	checklist := types.Checklist{
		{
			Name: "criu",
			List: []types.Check{
				criu.CheckVersion(s.plugins),
				criu.CheckFeatures(s.plugins, CRIU_EXPERIMENTAL_CHECKS),
			},
		},
	}

	if req.Full {
		checklist = append(checklist, s.pluginChecklist()...)
	}

	results := checklist.Run(ctx)

	return &daemon.HealthCheckResp{Results: results}, nil
}

func (s *Cedana) HealthCheck(ctx context.Context, req *daemon.HealthCheckReq) (*daemon.HealthCheckResp, error) {
	checklist := types.Checklist{
		{
			Name: "criu",
			List: []types.Check{
				criu.CheckVersion(s.plugins),
				criu.CheckFeatures(s.plugins, CRIU_EXPERIMENTAL_CHECKS),
			},
		},
	}

	if req.Full {
		checklist = append(checklist, s.pluginChecklist()...)
	}

	results := checklist.Run(ctx)

	return &daemon.HealthCheckResp{Results: results}, nil
}

func (s *Cedana) pluginChecklist() types.Checklist {
	checklist := []types.Checks{}

	// Add a criu/cuda health check if plugin is installed
	if s.plugins.IsInstalled("criu/cuda") {
		checklist = append(checklist, types.Checks{
			Name: "criu/cuda",
			List: []types.Check{
				criu.CheckCriuForCuda(s.plugins),
				criu.CheckCudaDriverVersion(),
			},
		})
	}

	// Add a GPU health check if plugin is installed
	if s.plugins.IsInstalled("gpu") {
		checklist = append(checklist, s.gpus.Checks())
	}

	// Add a streamer health check if plugin is installed
	if s.plugins.IsInstalled("streamer") {
		checklist = append(checklist, streamer.Checks(s.plugins))
	}

	// Add health checks from other plugins
	features.HealthChecks.IfAvailable(func(Name string, pluginChecks types.Checks) error {
		pluginChecks.Name = Name
		pluginChecks.List = slices.Insert(pluginChecks.List, 0, checkPluginVersion(s.plugins, Name))
		checklist = append(checklist, pluginChecks)
		return nil
	})

	return checklist
}

// Assumes plugin is installed
func checkPluginVersion(plugins plugins.Manager, plugin string) types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		component := &daemon.HealthCheckComponent{Name: "version"}

		p := plugins.Get(plugin)
		component.Data = p.Version

		return []*daemon.HealthCheckComponent{component}
	}
}
