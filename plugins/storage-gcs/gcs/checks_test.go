package gcs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cedana/cedana/pkg/config"
)

// A key that does not parse is reported unavailable, in either form
func TestCheckConfigReportsABadServiceAccountKey(t *testing.T) {
	settings := config.Global.GCS
	t.Cleanup(func() { config.Global.GCS = settings })

	file := filepath.Join(t.TempDir(), "key.json")
	if err := os.WriteFile(file, []byte(`{"type": "service_account", "private_key": "not a key"`), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, key := range map[string]string{
		"JSON":     `{"type": "service_account", "private_key": `,
		"File":     file,
		"NotThere": file + ".missing",
	} {
		t.Run(name, func(t *testing.T) {
			config.Global.GCS = config.GCS{CredentialsMode: CredentialsModeServiceAccount, ServiceAccountKey: key}
			components := CheckConfig()(context.Background())
			last := components[len(components)-1]
			if last.Name != "GCS Credentials" || last.Data != "unavailable" || len(last.Warnings) == 0 {
				t.Fatalf("component = %+v, want GCS Credentials unavailable with a warning", last)
			}
		})
	}
}

func TestLoadCredentialsParsesTheKey(t *testing.T) {
	if _, err := loadCredentials(config.GCS{CredentialsMode: CredentialsModeServiceAccount, ServiceAccountKey: `{"type": "service_account"`}); err == nil {
		t.Fatal("a malformed key loaded")
	}
	if _, err := loadCredentials(config.GCS{CredentialsMode: "bogus"}); err == nil {
		t.Fatal("an unknown mode loaded")
	}
}
