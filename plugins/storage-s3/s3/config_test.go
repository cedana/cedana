package s3

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cedanaconfig "github.com/cedana/cedana/pkg/config"
)

func TestLoadAWSConfigStatic(t *testing.T) {
	cfg, err := LoadAWSConfig(context.Background(), cedanaconfig.AWS{
		CredentialsMode: CredentialsModeStatic,
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "access" || creds.SecretAccessKey != "secret" {
		t.Fatal("static credentials were not selected")
	}
}

func TestLoadAWSConfigRejectsPartialStaticCredentials(t *testing.T) {
	for _, settings := range []cedanaconfig.AWS{
		{CredentialsMode: CredentialsModeStatic, AccessKeyID: "access"},
		{CredentialsMode: CredentialsModeStatic, SecretAccessKey: "secret"},
	} {
		if _, err := LoadAWSConfig(context.Background(), settings); err == nil {
			t.Fatal("expected partial static credentials to fail")
		}
	}
}

func TestLoadAWSConfigEKSPodIdentity(t *testing.T) {
	const token = "pod-identity-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != token {
			t.Errorf("authorization token = %q, want %q", got, token)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"AccessKeyId":"pod-access","SecretAccessKey":"pod-secret","Token":"session-token","Expiration":"2035-01-01T00:00:00Z"}`)
	}))
	t.Cleanup(server.Close)

	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", server.URL)
	t.Setenv("AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE", tokenFile)

	cfg, err := LoadAWSConfig(context.Background(), cedanaconfig.AWS{CredentialsMode: CredentialsModeEKSPodIdentity})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "pod-access" || creds.Source != "CredentialsEndpointProvider" {
		t.Fatalf("unexpected credentials: access key %q, source %q", creds.AccessKeyID, creds.Source)
	}
}

func TestLoadAWSConfigEKSPodIdentityRequiresInjection(t *testing.T) {
	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", "")
	t.Setenv("AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE", "")
	_, err := LoadAWSConfig(context.Background(), cedanaconfig.AWS{CredentialsMode: CredentialsModeEKSPodIdentity})
	if err == nil || !strings.Contains(err.Error(), "AWS_CONTAINER_CREDENTIALS_FULL_URI") {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", "http://127.0.0.1/credentials")
	_, err = LoadAWSConfig(context.Background(), cedanaconfig.AWS{CredentialsMode: CredentialsModeEKSPodIdentity})
	if err == nil || !strings.Contains(err.Error(), "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAWSConfigAmbientUsesDefaultChain(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ambient-secret")
	cfg, err := LoadAWSConfig(context.Background(), cedanaconfig.AWS{CredentialsMode: CredentialsModeAmbient})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "ambient-access" {
		t.Fatalf("access key = %q", creds.AccessKeyID)
	}
}

func TestLoadAWSConfigRejectsInvalidMode(t *testing.T) {
	_, err := LoadAWSConfig(context.Background(), cedanaconfig.AWS{CredentialsMode: "magic"})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unexpected error: %v", err)
	}
}
