package client

// Implements server-compatible health checks for runc

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/containerd/internal/defaults"
	"github.com/cedana/cedana/plugins/containerd/pkg/utils"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
)

// CheckVersion checks the versions associated with the containerd server
func CheckVersion() types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		ctx = namespaces.WithNamespace(ctx, defaults.DEFAULT_NAMESPACE)

		client, err := containerd.New(defaults.DEFAULT_ADDRESS, containerd.WithDefaultNamespace(defaults.DEFAULT_NAMESPACE))
		if err != nil {
			return []*daemon.HealthCheckComponent{{
				Name:   "containerd version",
				Data:   "unknown",
				Errors: []string{fmt.Sprintf("Error creating containerd client: %v", err)},
			}}
		}
		defer client.Close()

		version, err := client.Version(ctx)
		if err != nil {
			return []*daemon.HealthCheckComponent{{
				Name:   "containerd version",
				Data:   "unknown",
				Errors: []string{fmt.Sprintf("Error getting containerd version: %v", err)},
			}}
		}

		return []*daemon.HealthCheckComponent{
			{
				Name: "containerd version",
				Data: version.Version,
			}, {
				Name: "containerd revision",
				Data: version.Revision,
			},
		}
	}
}

// Checks if the containerd runtime is supported
func CheckRuntime() types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		components := []*daemon.HealthCheckComponent{}

		ctx = namespaces.WithNamespace(ctx, defaults.DEFAULT_NAMESPACE)

		client, err := containerd.New(defaults.DEFAULT_ADDRESS, containerd.WithDefaultNamespace(defaults.DEFAULT_NAMESPACE))
		if err != nil {
			return []*daemon.HealthCheckComponent{{
				Name:   "containerd runtime",
				Data:   "unknown",
				Errors: []string{fmt.Sprintf("Error creating containerd client: %v", err)},
			}}
		}
		defer client.Close()

		plugin := utils.PluginForRuntime(client.Runtime())

		dumpSupported, _ := features.DumpMiddleware.IsAvailable(plugin)
		restoreSupported, _ := features.RestoreMiddleware.IsAvailable(plugin)

		if !dumpSupported && !restoreSupported {
			return []*daemon.HealthCheckComponent{{
				Name:   "containerd runtime",
				Data:   client.Runtime(),
				Errors: []string{fmt.Sprintf("Unsupported runtime %s. Please install the %s plugin.", client.Runtime(), plugin)},
			}}
		}

		component := &daemon.HealthCheckComponent{
			Name: "containerd runtime",
			Data: client.Runtime(),
		}

		if !dumpSupported {
			component.Warnings = append(component.Warnings, fmt.Sprintf("Dump not supported by %s plugin.", plugin))
		}
		if !restoreSupported {
			component.Warnings = append(component.Warnings, fmt.Sprintf("Restore not supported by %s plugin.", plugin))
		}

		return append(components, component)
	}
}
