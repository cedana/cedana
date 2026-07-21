package s3

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
)

func CheckConfig() types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		components := []*daemon.HealthCheckComponent{{
			Name: "AWS Credentials Mode",
			Data: config.Global.AWS.CredentialsMode,
		}}
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

		credentialsComponent := &daemon.HealthCheckComponent{Name: "AWS Credentials", Data: "available"}
		cfg, err := LoadAWSConfig(ctx, config.Global.AWS)
		if err == nil {
			_, err = cfg.Credentials.Retrieve(ctx)
		}
		if err != nil {
			credentialsComponent.Data = "unavailable"
			credentialsComponent.Warnings = []string{err.Error()}
		}
		components = append(components, credentialsComponent)
		return components
	}
}
