package criu

import (
	"errors"
	"net"
	"os"
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
	path, err := externalsToConfig(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })

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
	path, err := externalsToConfig(opts)
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
