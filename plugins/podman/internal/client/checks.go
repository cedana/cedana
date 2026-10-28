package client

// Implements server-compatible health checks for podman

import (
	"context"
	"fmt"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/podman/pkg/utils"
)

// CheckVersion checks the version of podman
func CheckVersion() types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		out, err := utils.Podman(ctx, "version", "--format", "{{.Client.Version}}")
		if err != nil {
			return []*daemon.HealthCheckComponent{{
				Name:   "podman version",
				Data:   "unknown",
				Errors: []string{fmt.Sprintf("Error getting podman version: %v", err)},
			}}
		}

		return []*daemon.HealthCheckComponent{{
			Name: "podman version",
			Data: strings.TrimSpace(string(out)),
		}}
	}
}

// Checks if podman's default runtime is supported
func CheckRuntime() types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		out, err := utils.Podman(ctx, "info", "--format", "{{.Host.OCIRuntime.Name}}")
		if err != nil {
			return []*daemon.HealthCheckComponent{{
				Name:   "podman runtime",
				Data:   "unknown",
				Errors: []string{fmt.Sprintf("Error getting podman runtime: %v", err)},
			}}
		}
		runtime := strings.TrimSpace(string(out))

		if utils.RootForPlugin(runtime) == "" {
			return []*daemon.HealthCheckComponent{{
				Name:   "podman runtime",
				Data:   runtime,
				Errors: []string{fmt.Sprintf("Unsupported runtime %s.", runtime)},
			}}
		}

		dumpSupported, _ := features.DumpMiddleware.IsAvailable(runtime)
		restoreSupported, _ := features.RestoreMiddleware.IsAvailable(runtime)

		if !dumpSupported && !restoreSupported {
			return []*daemon.HealthCheckComponent{{
				Name:   "podman runtime",
				Data:   runtime,
				Errors: []string{fmt.Sprintf("Unsupported runtime %s. Please install the %s plugin.", runtime, runtime)},
			}}
		}

		component := &daemon.HealthCheckComponent{
			Name: "podman runtime",
			Data: runtime,
		}

		if !dumpSupported {
			component.Warnings = append(component.Warnings, fmt.Sprintf("Dump not supported by %s plugin.", runtime))
		}
		if !restoreSupported {
			component.Warnings = append(component.Warnings, fmt.Sprintf("Restore not supported by %s plugin.", runtime))
		}

		return []*daemon.HealthCheckComponent{component}
	}
}
