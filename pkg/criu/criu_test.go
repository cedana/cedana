package criu

import (
	"errors"
	"net"
	"os"
	"slices"
	"syscall"
	"testing"
)

func seqpacketPair(t *testing.T) (*net.UnixConn, *os.File) {
	t.Helper()
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
	return conn.(*net.UnixConn), srv
}

func TestGrowSendBufferFitsLargeMessage(t *testing.T) {
	conn, _ := seqpacketPair(t)

	var cur int
	raw, _ := conn.SyscallConn()
	raw.Control(func(fd uintptr) {
		cur, _ = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_SNDBUF)
	})
	msg := make([]byte, cur) // > sk_sndbuf - 32, so the kernel rejects it

	_, _, err := conn.WriteMsgUnix(msg, nil, nil)
	if !errors.Is(err, syscall.EMSGSIZE) {
		t.Fatalf("expected EMSGSIZE before growing, got %v", err)
	}

	if err := growSendBuffer(conn, len(msg)); err != nil {
		t.Fatal(err)
	}
	if n, _, err := conn.WriteMsgUnix(msg, nil, nil); err != nil || n != len(msg) {
		t.Fatalf("write after grow: n=%d err=%v", n, err)
	}
}

func TestDedupe(t *testing.T) {
	got := dedupe([]string{"mnt[/a]:/a", "file[1:2]", "mnt[/a]:/a", "mnt[/b]:/b", "file[1:2]"})
	want := []string{"mnt[/a]:/a", "file[1:2]", "mnt[/b]:/b"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
