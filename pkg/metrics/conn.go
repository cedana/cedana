package metrics

import (
	"io"
	"net/http"
	"sync"

	"github.com/cedana/cedana/pkg/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// The exporters would otherwise each connect at their first export, which for a short-lived
// CLI run is the flush at exit: a TCP and TLS handshake to SigNoz (~300 ms) each, on the
// critical path of every container a node starts. Connecting early overlaps the handshakes
// with the work the process does meanwhile.

// dialOTLP opens, once, the gRPC connection traces and metrics are exported through, and
// starts connecting right away.
var dialOTLP = sync.OnceValues(func() (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		Credentials.Endpoint,
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	return conn, nil
})

// warmLogsConn opens, once, the HTTPS connection logs are sent through (see initLogger),
// for the shared transport to keep and reuse.
var warmLogsConn = sync.OnceFunc(func() {
	go func() {
		req, err := http.NewRequest(http.MethodHead, logsEndpoint(), nil)
		if err != nil {
			return
		}
		resp, err := (&http.Client{Timeout: EXPORT_TIMEOUT}).Do(req)
		if err != nil {
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
})

func logsEndpoint() string {
	return "https://" + Credentials.Endpoint + "/logs/json"
}

// Prewarm starts connecting to the telemetry backend ahead of Init, so the handshakes
// also overlap whatever the caller does before calling Init (such as gathering resource
// attributes). Only with cached credentials: fetching them is what Init must not wait
// on twice.
func Prewarm() {
	if Credentials == nil {
		url, authToken := config.Global.Connection.URL, config.Global.Connection.AuthToken
		if url == "" || authToken == "" {
			return
		}
		cached := loadCachedCreds(url, authToken)
		if cached == nil {
			return
		}
		Credentials = cached
	}
	dialOTLP()
	warmLogsConn()
}
