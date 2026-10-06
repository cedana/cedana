package checksum

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"buf.build/gen/go/cedana/cedana/grpc/go/daemon/daemongrpc"
	"github.com/cedana/cedana/internal/cedana"
	"github.com/cedana/cedana/pkg/client"
	"google.golang.org/grpc"
)

// The read back of a local checkpoint as the helper does it: through the daemon's
// own ListPath and ReadPath handlers, served over gRPC on a unix socket, so the
// cost of the stream is in the number. CEDANA_BENCH_FILE names the checkpoint
// file to read back; skipped when it is not set.
func BenchmarkReadBack(b *testing.B) {
	path := os.Getenv("CEDANA_BENCH_FILE")
	if path == "" {
		b.Skip("CEDANA_BENCH_FILE not set")
	}
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}

	sock := filepath.Join(b.TempDir(), "daemon.sock")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		b.Fatal(err)
	}
	server := grpc.NewServer()
	daemongrpc.RegisterDaemonServer(server, &cedana.Server{})
	go server.Serve(listener)
	b.Cleanup(server.Stop)

	c, err := client.New(sock, "unix")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { c.Close() })

	// A tarball is read whole; anything else is listed and read entry by entry
	b.SetBytes(info.Size())
	b.ResetTimer()
	for b.Loop() {
		if _, err := Path(context.Background(), c, path); err != nil {
			b.Fatal(err)
		}
	}
}
