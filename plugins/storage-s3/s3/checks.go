package s3

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
)

func CheckConfig() types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		components := []*daemon.HealthCheckComponent{}

		if config.Global.AWS.AccessKeyID == "" {
			components = append(components, &daemon.HealthCheckComponent{
				Name:   "AWS AccessKeyID",
				Data:   "not set",
				Warnings: []string{"AWS AccessKeyID is not set in the configuration"},
			})
		} else {
			components = append(components, &daemon.HealthCheckComponent{
				Name: "AWS AccessKeyID",
				Data: "set",
			})
		}
		if config.Global.AWS.SecretAccessKey == "" {
			components = append(components, &daemon.HealthCheckComponent{
				Name:   "AWS SecretAccessKey",
				Data:   "not set",
				Warnings: []string{"AWS SecretAccessKey is not set in the configuration"},
			})
		} else {
			components = append(components, &daemon.HealthCheckComponent{
				Name: "AWS SecretAccessKey",
				Data: "set",
			})
		}
		if config.Global.AWS.Region == "" {
			components = append(components, &daemon.HealthCheckComponent{
				Name:     "AWS Region",
				Data:     "not set",
				Warnings: []string{"AWS Region is not set in the configuration"},
			})
		} else {
			components = append(components, &daemon.HealthCheckComponent{
				Name: "AWS Region",
				Data: config.Global.AWS.Region,
			})
		}
		return components
	}
}
