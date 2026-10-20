package cedana

import (
	"net"
	"os"
	"path/filepath"
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
