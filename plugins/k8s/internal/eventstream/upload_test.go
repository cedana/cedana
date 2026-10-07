package eventstream

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"buf.build/gen/go/cedana/cedana/grpc/go/daemon/daemongrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	propagatorsdk "github.com/cedana/cedana-propagator-sdk/go"
	"github.com/cedana/cedana/pkg/client"
	"github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Daemon that answers WaitUpload with a fixed response and serves fixed files
type uploadDaemon struct {
	daemongrpc.UnimplementedDaemonServer
	resp     *daemon.WaitUploadResp
	notFound bool // answer WaitUpload with NotFound, as a daemon that lost its record does
}

func (d *uploadDaemon) WaitUpload(_ context.Context, req *daemon.WaitUploadReq) (*daemon.WaitUploadResp, error) {
	if d.notFound {
		return nil, status.Errorf(codes.NotFound, "no upload found for path %s", req.GetPath())
	}
	return d.resp, nil
}

// Propagator that records what it is asked to mark as uploaded
type uploadPropagator struct {
	sync.Mutex
	requests map[string]map[string]any // by request path
}

func (p *uploadPropagator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The SDK compresses the bodies of its requests
	var reader io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		reader, _ = gzip.NewReader(r.Body)
	}
	body, _ := io.ReadAll(reader)
	var fields map[string]any
	json.Unmarshal(body, &fields)

	p.Lock()
	p.requests[r.Method+" "+r.URL.Path] = fields
	p.Unlock()

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(filepath.Base(r.URL.Path)))
}

func newUploadEventStream(t *testing.T, resp *daemon.WaitUploadResp) (*EventStream, *uploadPropagator) {
	return newEventStreamWithDaemon(t, &uploadDaemon{resp: resp})
}

func newEventStreamWithDaemon(t *testing.T, d *uploadDaemon) (*EventStream, *uploadPropagator) {
	sock := filepath.Join(t.TempDir(), "daemon.sock")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	daemongrpc.RegisterDaemonServer(server, d)
	go server.Serve(listener)
	t.Cleanup(server.Stop)

	cedana, err := client.New(sock, "unix")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cedana.Close() })

	propagator := &uploadPropagator{requests: map[string]map[string]any{}}
	api := httptest.NewServer(propagator)
	t.Cleanup(api.Close)

	return &EventStream{
		cedana:     cedana,
		propagator: propagatorsdk.NewClient(api.URL, "token"),
	}, propagator
}

func TestReportUpload(t *testing.T) {
	ctx := context.Background()
	spec := &specs.Spec{Process: &specs.Process{}}
	const checkpointId = "394f8cdf-881e-4cd8-8c6c-c22a187863f9"
	const path = "s3://checkpoints/394f8cdf-881e-4cd8-8c6c-c22a187863f9.tar.lz4"

	t.Run("Succeeded", func(t *testing.T) {
		es, propagator := newUploadEventStream(t, &daemon.WaitUploadResp{})

		es.reportUpload(ctx, "pod", "action", checkpointId, path, &daemon.ProcessState{}, 0, spec)

		request, ok := propagator.requests["POST /v1/checkpoints/uploaded/"+checkpointId]
		if !ok {
			t.Fatalf("checkpoint was not marked as uploaded, got requests %v", propagator.requests)
		}
		if request["restore_path"] != path {
			t.Fatalf("expected restore path %s, got %v", path, request["restore_path"])
		}
		if _, ok := request["checksum"]; ok {
			t.Fatalf("the helper has no checksum to send, got %v", request["checksum"])
		}
	})

	t.Run("Failed", func(t *testing.T) {
		es, propagator := newUploadEventStream(t, &daemon.WaitUploadResp{Error: "connection reset"})

		es.reportUpload(ctx, "pod", "action", checkpointId, path, &daemon.ProcessState{}, 0, spec)

		if len(propagator.requests) != 0 {
			t.Fatalf("a failed upload must not be marked as uploaded, got requests %v", propagator.requests)
		}
	})

	t.Run("SucceededWithChecksum", func(t *testing.T) {
		es, propagator := newUploadEventStream(t, &daemon.WaitUploadResp{Checksum: "crc32c:0000abcd"})

		es.reportUpload(ctx, "pod", "action", checkpointId, path, &daemon.ProcessState{}, 0, spec)

		request := propagator.requests["POST /v1/checkpoints/uploaded/"+checkpointId]
		if request["checksum"] != "crc32c:0000abcd" {
			t.Fatalf("expected the daemon's checksum to be reported, got %v", request["checksum"])
		}
	})

	t.Run("DaemonLostTheRecord", func(t *testing.T) {
		es, propagator := newEventStreamWithDaemon(t, &uploadDaemon{notFound: true})

		es.reportUpload(ctx, "pod", "action", checkpointId, path, &daemon.ProcessState{}, 0, spec)

		// A daemon that restarted knows nothing of the path: the checkpoint cannot be vouched for
		if _, ok := propagator.requests["POST /v1/checkpoints/uploaded/"+checkpointId]; ok {
			t.Fatalf("a checkpoint the daemon lost must not be marked as uploaded, got %v", propagator.requests)
		}
	})
}
