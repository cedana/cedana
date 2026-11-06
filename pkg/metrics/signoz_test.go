package metrics

import "testing"

// Endpoints are accepted with or without a scheme and port
func TestHostPort(t *testing.T) {
	for _, endpoint := range []string{
		"ingest.us.signoz.cloud",
		"ingest.us.signoz.cloud:443",
		"https://ingest.us.signoz.cloud",
		"https://ingest.us.signoz.cloud/",
		"https://ingest.us.signoz.cloud:443",
	} {
		if got := hostPort(endpoint); got != "ingest.us.signoz.cloud:443" {
			t.Errorf("hostPort(%q) = %q, want ingest.us.signoz.cloud:443", endpoint, got)
		}
	}
}
