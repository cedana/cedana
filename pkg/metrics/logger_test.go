package metrics

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cedana/cedana/pkg/logging"
	"go.opentelemetry.io/otel"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// A line with its own service is sent as that service, with the version of its plugin
func TestSignozWriterService(t *testing.T) {
	sw := &signozWriter{
		resource: map[string]string{
			string(semconv.ServiceNameKey):    "cedana",
			string(semconv.ServiceVersionKey): "v1.2.3",
			string(semconv.HostNameKey):       "node",
			"cedana.plugin.criu.version":      "v4.2.1",
		},
		services: make(map[string]map[string]string),
	}

	sw.Write([]byte(`{"level":"info","message":"daemon"}`))
	sw.Write([]byte(`{"level":"debug","message":"gpu","service.name":"cedana-gpu","plugin":"gpu","ID":"abc"}`))
	sw.Write([]byte(`{"level":"info","message":"criu","service.name":"cedana-criu","plugin":"CRIU"}`))

	daemon, gpu, criu := sw.logBuffer[0], sw.logBuffer[1], sw.logBuffer[2]
	if daemon.Resources[string(semconv.ServiceNameKey)] != "cedana" || daemon.Resources[string(semconv.ServiceVersionKey)] != "v1.2.3" {
		t.Errorf("daemon resource changed: %v", daemon.Resources)
	}
	if gpu.Resources[string(semconv.ServiceNameKey)] != "cedana-gpu" {
		t.Errorf("service.name = %q, want cedana-gpu", gpu.Resources[string(semconv.ServiceNameKey)])
	}
	if _, ok := gpu.Resources[string(semconv.ServiceVersionKey)]; ok {
		t.Errorf("GPU line has a version though its plugin's is unknown: %v", gpu.Resources)
	}
	if criu.Resources[string(semconv.ServiceNameKey)] != "cedana-criu" || criu.Resources[string(semconv.ServiceVersionKey)] != "v4.2.1" {
		t.Errorf("CRIU line resource = %v, want cedana-criu at v4.2.1", criu.Resources)
	}
	if gpu.Resources[string(semconv.HostNameKey)] != "node" || gpu.Attributes["ID"] != "abc" {
		t.Errorf("GPU line lost context: resource %v, attributes %v", gpu.Resources, gpu.Attributes)
	}
	if _, ok := gpu.Attributes["service.name"]; ok {
		t.Errorf("service.name left in attributes: %v", gpu.Attributes)
	}
}

// captureStdio returns what f writes to stdout and stderr.
func captureStdio(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()
	f()
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// SigNoz refusing a batch (a revoked key) never reaches stdout or stderr: a shim's are its
// reply to containerd, and the error glued onto the address it returned broke every
// container start. It goes to the process's own log instead.
func TestExportFailuresStayOffStdio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Invalid or missing key"))
	}))
	defer srv.Close()

	var own bytes.Buffer
	writer, level := logging.GlobalWriter, logging.Level
	defer func() { logging.GlobalWriter, logging.Level = writer, level }()
	logging.GlobalWriter, logging.Level = &own, 0
	initLocal("test")

	sw := &signozWriter{httpClient: srv.Client(), endpoint: srv.URL, logBuffer: []signozLogEntry{{Body: "hello"}}}
	out := captureStdio(t, func() {
		sw.flushBuffer()
		sw.Write([]byte("not json"))
		otel.Handle(errors.New("rpc error: code = Unauthenticated desc = Invalid or missing key"))
	})
	if out != "" {
		t.Errorf("telemetry wrote %q to stdout/stderr", out)
	}
	for _, want := range []string{"SigNoz refused a log batch", "Invalid or missing key", "dropping a log line", "telemetry export failed"} {
		if !strings.Contains(own.String(), want) {
			t.Errorf("the process's log lacks %q: %s", want, own.String())
		}
	}
}
