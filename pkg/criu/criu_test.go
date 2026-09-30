package criu

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"google.golang.org/protobuf/proto"
)

func TestDedupe(t *testing.T) {
	got := dedupe([]string{"mnt[/a]:/a", "file[1:2]", "mnt[/a]:/a", "mnt[/b]:/b", "file[1:2]"})
	want := []string{"mnt[/a]:/a", "file[1:2]", "mnt[/b]:/b"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestExternalsToConfig(t *testing.T) {
	prev, err := os.CreateTemp(t.TempDir(), "prev.conf")
	if err != nil {
		t.Fatal(err)
	}
	prev.WriteString("tcp-close")
	prev.Close()

	opts := &criu.CriuOpts{
		ConfigFile: proto.String(prev.Name()),
		External:   []string{"mnt[/a]:/a", "mnt[/has space]:/x", "file[1:2]", "mnt[/has#hash]:/y"},
	}
	dir := t.TempDir()
	path, err := externalsToConfig(opts, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })

	if filepath.Dir(path) != dir {
		t.Fatalf("config %q not created in %q", path, dir)
	}
	if opts.GetConfigFile() != path {
		t.Fatalf("ConfigFile not updated: %q", opts.GetConfigFile())
	}
	if want := []string{"mnt[/has space]:/x", "mnt[/has#hash]:/y"}; !slices.Equal(opts.External, want) {
		t.Fatalf("inline externals %v want %v", opts.External, want)
	}
	b, _ := os.ReadFile(path)
	if want := "tcp-close\nexternal mnt[/a]:/a\nexternal file[1:2]\n"; string(b) != want {
		t.Fatalf("config:\n%s\nwant:\n%s", b, want)
	}
}

func TestExternalsToConfigNoExternals(t *testing.T) {
	opts := &criu.CriuOpts{ConfigFile: proto.String("/keep/me")}
	path, err := externalsToConfig(opts, "")
	if err != nil || path != "" || opts.GetConfigFile() != "/keep/me" {
		t.Fatalf("path=%q cfg=%q err=%v", path, opts.GetConfigFile(), err)
	}
}

func TestSendAndRecvReportsOversizedRequest(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	cln := os.NewFile(uintptr(fds[0]), "cln")
	defer cln.Close()
	conn, err := net.FileConn(cln)
	if err != nil {
		t.Fatal(err)
	}
	srv := os.NewFile(uintptr(fds[1]), "srv")
	t.Cleanup(func() { conn.Close(); srv.Close() })

	c := &Criu{swrkSk: conn.(*net.UnixConn)}
	var sndbuf int
	raw, _ := c.swrkSk.SyscallConn()
	raw.Control(func(fd uintptr) {
		sndbuf, _ = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_SNDBUF)
	})

	_, _, _, _, err = c.sendAndRecv(make([]byte, sndbuf)) // > sk_sndbuf-32
	if !errors.Is(err, syscall.EMSGSIZE) || !strings.Contains(err.Error(), "exceeds the socket send buffer") {
		t.Fatalf("got %v", err)
	}
}

func TestInMountNamespace(t *testing.T) {
	inside, err := inMountNamespace("/proc/self/ns/mnt")
	if err != nil || !inside {
		t.Errorf("expected to be in our own mount namespace, got %v, %v", inside, err)
	}
	inside, err = inMountNamespace("/proc/self/ns/pid") // another namespace file, so another inode
	if err != nil || inside {
		t.Errorf("expected another namespace not to count as ours, got %v, %v", inside, err)
	}
	if _, err := inMountNamespace("/proc/self/ns/no-such-namespace"); err == nil {
		t.Error("expected an error for a path that isn't there")
	}
}
