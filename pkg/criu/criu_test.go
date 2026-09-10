package criu

import (
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"syscall"
	"testing"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"google.golang.org/protobuf/proto"
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

func sndbuf(t *testing.T, conn *net.UnixConn) (cur int) {
	t.Helper()
	raw, _ := conn.SyscallConn()
	raw.Control(func(fd uintptr) {
		cur, _ = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_SNDBUF)
	})
	return cur
}

func TestGrowSendBufferFitsLargeMessage(t *testing.T) {
	conn, _ := seqpacketPair(t)
	msg := make([]byte, sndbuf(t, conn)) // > sk_sndbuf - 32, so the kernel rejects it

	_, _, err := conn.WriteMsgUnix(msg, nil, nil)
	if !errors.Is(err, syscall.EMSGSIZE) {
		t.Fatalf("expected EMSGSIZE before growing, got %v", err)
	}

	avail, err := growSendBuffer(conn, len(msg))
	if err != nil {
		t.Fatal(err)
	}
	if avail < len(msg) {
		t.Fatalf("reported avail %d < %d", avail, len(msg))
	}
	if n, _, err := conn.WriteMsgUnix(msg, nil, nil); err != nil || n != len(msg) {
		t.Fatalf("write after grow: n=%d err=%v", n, err)
	}
}

func TestGrowSendBufferReportsClamp(t *testing.T) {
	conn, _ := seqpacketPair(t)
	// Far beyond any wmem_max; the kernel clamps SO_SNDBUF and must not be trusted.
	avail, err := growSendBuffer(conn, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if avail >= 1<<30 {
		t.Fatalf("avail %d should reflect the clamped buffer", avail)
	}
	if avail != sndbuf(t, conn)-32 {
		t.Fatalf("avail %d does not match SO_SNDBUF-32 (%d)", avail, sndbuf(t, conn)-32)
	}
}

func TestDedupe(t *testing.T) {
	got := dedupe([]string{"mnt[/a]:/a", "file[1:2]", "mnt[/a]:/a", "mnt[/b]:/b", "file[1:2]", "old"}, map[string]struct{}{"old": {}})
	want := []string{"mnt[/a]:/a", "file[1:2]", "mnt[/b]:/b"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSpillExternals(t *testing.T) {
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
	path, err := spillExternals(opts)
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

func TestMarshalToFitSpillsWhenTooLarge(t *testing.T) {
	conn, _ := seqpacketPair(t)
	c := &Criu{swrkSk: conn}

	limit, err := growSendBuffer(conn, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if limit >= 1<<30 {
		t.Skip("CAP_NET_ADMIN available, buffer is not clamped")
	}

	// Enough unique keys to overshoot the clamped buffer.
	key := "mnt[/usr/lib/x86_64-linux-gnu/libcuda.so.%08d]:/usr/lib/x86_64-linux-gnu/libcuda.so.%08d"
	var keys []string
	for i := 0; len(keys)*len(key) < 2*limit; i++ {
		keys = append(keys, fmt.Sprintf(key, i, i))
	}
	req := &criu.CriuReq{
		Type: criu.CriuReqType_DUMP.Enum(),
		Opts: &criu.CriuOpts{ImagesDirFd: proto.Int32(-1), External: keys},
	}
	sent := map[string]struct{}{}
	reqB, cfgPath, err := c.marshalToFit(req, sent)
	if cfgPath != "" {
		t.Cleanup(func() { os.Remove(cfgPath) })
	}
	if err != nil {
		t.Fatal(err)
	}
	if cfgPath == "" || req.Opts.GetConfigFile() != cfgPath {
		t.Fatal("expected externals to be spilled into a config file")
	}
	if len(req.Opts.External) != 0 {
		t.Fatalf("expected all keys spilled, %d left inline", len(req.Opts.External))
	}
	if len(sent) != len(keys) {
		t.Fatalf("sent tracks %d keys, want %d", len(sent), len(keys))
	}
	if n, _, err := conn.WriteMsgUnix(reqB, nil, nil); err != nil || n != len(reqB) {
		t.Fatalf("write: n=%d err=%v", n, err)
	}

	// A notify reply carrying only already-sent keys shrinks to nothing.
	reply := &criu.CriuReq{
		Type: criu.CriuReqType_NOTIFY.Enum(),
		Opts: &criu.CriuOpts{ImagesDirFd: proto.Int32(-1), External: append([]string{"file[9:9]"}, keys...)},
	}
	if _, p, err := c.marshalToFit(reply, sent); err != nil || p != "" {
		t.Fatalf("reply: path=%q err=%v", p, err)
	}
	if !slices.Equal(reply.Opts.External, []string{"file[9:9]"}) {
		t.Fatalf("reply externals %v", reply.Opts.External)
	}
}
