package gcs

import (
	"context"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/storage"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
)

func CheckConfig() types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		settings := config.Global.GCS
		components := []*daemon.HealthCheckComponent{{
			Name: "GCS Credentials Mode",
			Data: settings.CredentialsMode,
		}}
		if settings.EmulatorHost != "" {
			components = append(components, &daemon.HealthCheckComponent{
				Name:     "GCS Emulator",
				Data:     settings.EmulatorHost,
				Warnings: []string{"GCS requests go to an emulator, without credentials"},
			})
			return components
		}

		credentialsComponent := &daemon.HealthCheckComponent{Name: "GCS Credentials", Data: "available"}
		_, err := ClientOptions(settings)
		if err == nil && settings.CredentialsMode == CredentialsModeAmbient {
			detectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			creds, detectErr := credentials.DetectDefault(&credentials.DetectOptions{Scopes: []string{storage.ScopeFullControl}})
			if detectErr == nil {
				_, detectErr = creds.Token(detectCtx)
			}
			err = detectErr
		}
		if err != nil {
			credentialsComponent.Data = "unavailable"
			credentialsComponent.Warnings = []string{err.Error()}
		}
		components = append(components, credentialsComponent)
		return components
	}
}
