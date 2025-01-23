package streamer

// Health checks for streamer

import (
	"context"
	"os/exec"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
)

func Checks(m plugins.Manager) types.Checks {
	return types.Checks{
		Name: "streamer",
		List: []types.Check{
			CheckVersion(m),
			CheckCriu(m),
		},
	}
}

func CheckVersion(manager plugins.Manager) types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		component := &daemon.HealthCheckComponent{Name: "version"}

		// Check if CRIU plugin is installed, then use that binary
		var p *plugins.Plugin
		if p = manager.Get("streamer"); p.IsInstalled() {
			component.Data = p.Version
		} else {
			component.Errors = append(component.Errors, "Streamer plugin is not installed. This is required for streaming C/R support.")
			component.Data = "unknown"
		}

		return []*daemon.HealthCheckComponent{component}
	}
}

func CheckCriu(manager plugins.Manager) types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		component := &daemon.HealthCheckComponent{Name: "criu"}

		// Check if CRIU plugin is installed
		var p *plugins.Plugin
		if p = manager.Get("criu"); p.IsInstalled() {
			component.Data = "supported"
		} else {
			if custom_path := config.Global.CRIU.BinaryPath; custom_path != "" {
				component.Errors = append(component.Warnings,
					"CRIU plugin not installed but a custom CRIU path was provided. Streaming C/R requires the CRIU plugin.",
				)
				component.Data = "unsupported"
			} else if _, err := exec.LookPath("criu"); err == nil {
				component.Errors = append(component.Warnings,
					"CRIU plugin not installed but CRIU binary found in PATH. Streaming C/R requires the CRIU plugin.",
				)
				component.Data = "unsupported"
			} else {
				component.Errors = append(component.Errors,
					"CRIU plugin is not installed. This is required for userspace C/R support.",
				)
				component.Data = "missing"
			}
		}

		return []*daemon.HealthCheckComponent{component}
	}
}
