package gcs

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"cloud.google.com/go/storage"
	cedanaconfig "github.com/cedana/cedana/pkg/config"
	"google.golang.org/api/option"
)

const (
	CredentialsModeAmbient        = "ambient"
	CredentialsModeServiceAccount = "serviceAccount"
)

// ClientOptions validates the selected authentication mode and returns the options of
// the GCS client shared by the storage and its health check.
func ClientOptions(settings cedanaconfig.GCS) ([]option.ClientOption, error) {
	if host := strings.TrimSpace(settings.EmulatorHost); host != "" {
		// As the SDK does for STORAGE_EMULATOR_HOST: the emulator takes no credentials.
		// Reads go through the JSON API too, since the SDK's default XML reads do not
		// follow a custom endpoint.
		endpoint, err := emulatorEndpoint(host)
		if err != nil {
			return nil, err
		}
		return []option.ClientOption{option.WithEndpoint(endpoint), option.WithoutAuthentication(), storage.WithJSONReads()}, nil
	}

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

// emulatorEndpoint turns an emulator host, with or without a scheme, into the JSON API endpoint
func emulatorEndpoint(host string) (string, error) {
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("invalid GCS emulator_host %q: %w", host, err)
	}
	u.Path = "/storage/v1/"
	return u.String(), nil
}
