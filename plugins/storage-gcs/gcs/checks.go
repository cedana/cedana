package gcs

import (
	"context"
	"os"
	"strings"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"cloud.google.com/go/auth"
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
		// The SDK sends every request to an emulator, without credentials, when its
		// variable is set; a variable left set would redirect the checkpoints silently
		if host := os.Getenv(EMULATOR_HOST_ENV); host != "" {
			components = append(components, &daemon.HealthCheckComponent{
				Name:     "GCS Emulator",
				Data:     host,
				Warnings: []string{"GCS requests go to an emulator, without credentials"},
			})
			return components
		}

		credentialsComponent := &daemon.HealthCheckComponent{Name: "GCS Credentials", Data: "available"}
		creds, err := loadCredentials(settings)
		if err == nil {
			tokenCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_, err = creds.Token(tokenCtx)
		}
		if err != nil {
			credentialsComponent.Data = "unavailable"
			credentialsComponent.Warnings = []string{err.Error()}
		}
		components = append(components, credentialsComponent)
		return components
	}
}

// loadCredentials loads the credentials of the selected mode, as the client would:
// the default chain for ambient, the key for serviceAccount. A malformed or missing
// key fails here, before any request.
func loadCredentials(settings config.GCS) (*auth.Credentials, error) {
	if _, err := ClientOptions(settings); err != nil {
		return nil, err
	}
	opts := &credentials.DetectOptions{Scopes: []string{storage.ScopeFullControl}}
	if settings.CredentialsMode == CredentialsModeServiceAccount {
		key := strings.TrimSpace(settings.ServiceAccountKey)
		if strings.HasPrefix(key, "{") {
			opts.CredentialsJSON = []byte(key)
		} else {
			opts.CredentialsFile = key
		}
	}
	return credentials.DetectDefault(opts)
}
