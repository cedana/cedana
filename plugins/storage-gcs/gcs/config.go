package gcs

import (
	"fmt"
	"os"
	"strings"

	cedanaconfig "github.com/cedana/cedana/pkg/config"
	"google.golang.org/api/option"
)

const (
	CredentialsModeAmbient        = "ambient"
	CredentialsModeServiceAccount = "serviceAccount"
)

// EMULATOR_HOST_ENV is the SDK's own variable for a GCS emulator such as fake-gcs-server:
// when it is set, the client sends every request to that host, without credentials.
// It is for testing.
const EMULATOR_HOST_ENV = "STORAGE_EMULATOR_HOST"

// ClientOptions validates the selected authentication mode and returns the options of
// the GCS client shared by the storage and its health check.
func ClientOptions(settings cedanaconfig.GCS) ([]option.ClientOption, error) {
	switch settings.CredentialsMode {
	case CredentialsModeAmbient:
		// The default chain: Workload Identity on GKE, the metadata server,
		// GOOGLE_APPLICATION_CREDENTIALS, gcloud's application default credentials
		return nil, nil
	case CredentialsModeServiceAccount:
		key := strings.TrimSpace(settings.ServiceAccountKey)
		if key == "" {
			return nil, fmt.Errorf("GCS service_account_key must be set when credentials_mode is %q", CredentialsModeServiceAccount)
		}
		if strings.HasPrefix(key, "{") {
			return []option.ClientOption{option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(key))}, nil
		}
		if _, err := os.Stat(key); err != nil {
			return nil, fmt.Errorf("GCS service_account_key file: %w", err)
		}
		return []option.ClientOption{option.WithAuthCredentialsFile(option.ServiceAccount, key)}, nil
	default:
		return nil, fmt.Errorf("unsupported GCS credentials_mode %q (expected ambient or serviceAccount)", settings.CredentialsMode)
	}
}
