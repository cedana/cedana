package metrics

import (
	"testing"

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
