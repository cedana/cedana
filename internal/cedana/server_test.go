package cedana

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestListenUnix(t *testing.T) {
	address := filepath.Join(t.TempDir(), "missing", "cedana.sock")

	// Creates the missing directory
	listener, err := listenUnix(address)
	if err != nil {
		t.Fatal(err)
	}

	// Refuses to take over a socket that is still being served
	if _, err := listenUnix(address); err == nil {
		t.Fatal("expected an error while another listener is serving")
	}

	// Replaces a stale socket left behind by a killed daemon
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	listener.Close()
	if _, err := os.Stat(address); err != nil {
		t.Fatalf("expected a stale socket file: %v", err)
	}
	listener, err = listenUnix(address)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	conn, err := net.Dial("unix", address)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestListenUnixKeepsNonSocket(t *testing.T) {
	address := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(address, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := listenUnix(address); err == nil {
		t.Fatal("expected an error for a path that is not a socket")
	}
	if data, err := os.ReadFile(address); err != nil || string(data) != "x" {
		t.Fatalf("expected the file to be kept, got %q, %v", data, err)
	}
}

func TestListenUnixConcurrent(t *testing.T) {
	address := filepath.Join(t.TempDir(), "cedana.sock")

	const starts = 20
	listeners := make(chan net.Listener, starts)
	var wg sync.WaitGroup
	for range starts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if listener, err := listenUnix(address); err == nil {
				listeners <- listener
			}
		}()
	}
	wg.Wait()
	close(listeners)

	if len(listeners) != 1 {
		t.Fatalf("expected exactly one listener, got %d", len(listeners))
	}
	for listener := range listeners {
		listener.Close()
	}
}
