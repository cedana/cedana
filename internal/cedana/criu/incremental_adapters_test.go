package criu

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Records the CRIU opts the final handler saw, and writes an image like CRIU would.
type incrementalHarness struct {
	t        *testing.T
	root     string
	pid      uint32
	start    uint64
	children []*daemon.ProcessState // the dumped process's descendants
	failErr  error
	seen     *criu_proto.CriuOpts
}

func resetChains() {
	chainsMu.Lock()
	defer chainsMu.Unlock()
	clear(incrementalChains)
}

func newIncrementalHarness(t *testing.T) *incrementalHarness {
	t.Helper()
	resetChains()
	orig := memTrackSupported
	memTrackSupported = func(context.Context, *criu.Criu) (bool, error) { return true, nil }
	t.Cleanup(func() {
		memTrackSupported = orig
		resetChains()
	})
	return &incrementalHarness{t: t, root: t.TempDir(), pid: 4242, start: 1000}
}

func (h *incrementalHarness) handler() types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (func() <-chan int, error) {
		h.seen = proto.Clone(req.GetCriu()).(*criu_proto.CriuOpts)
		if h.failErr != nil {
			return nil, h.failErr
		}
		dir := req.GetCriu().GetImagesDir()
		if err := os.WriteFile(filepath.Join(dir, criuInventoryImage), nil, 0o644); err != nil {
			h.t.Fatal(err)
		}
		return nil, nil
	}
}

// dump runs one dump into <root>/<name> and returns the response.
func (h *incrementalHarness) dump(name string, trackMem bool, mutate func(*daemon.DumpReq)) (*daemon.DumpResp, error) {
	h.t.Helper()
	dir := filepath.Join(h.root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	req := &daemon.DumpReq{
		Compression: "none",
		Criu: &criu_proto.CriuOpts{
			ImagesDir: proto.String(dir),
			TrackMem:  proto.Bool(trackMem),
		},
	}
	if mutate != nil {
		mutate(req)
	}
	resp := &daemon.DumpResp{State: &daemon.ProcessState{PID: h.pid, StartTime: h.start, Children: h.children}}
	_, err := IncrementalForDump(h.handler())(context.Background(), types.Opts{}, resp, req)
	return resp, err
}

func mustDump(t *testing.T, h *incrementalHarness, name string, mutate func(*daemon.DumpReq)) {
	t.Helper()
	if _, err := h.dump(name, true, mutate); err != nil {
		t.Fatalf("dump %s: %v", name, err)
	}
}

func TestIncrementalChainsOntoPreviousDump(t *testing.T) {
	h := newIncrementalHarness(t)

	mustDump(t, h, "d1", nil)
	if h.seen.ParentImg != nil {
		t.Fatalf("base dump got parent %q", h.seen.GetParentImg())
	}
	if !h.seen.GetTrackMem() {
		t.Fatal("base dump must track memory so the next dump can be a delta")
	}

	mustDump(t, h, "d2", nil)
	if got := h.seen.GetParentImg(); got != "../d1" {
		t.Fatalf("parent = %q, want relative ../d1", got)
	}

	mustDump(t, h, "d3", nil)
	if got := h.seen.GetParentImg(); got != "../d2" {
		t.Fatalf("parent = %q, want ../d2 (the latest link)", got)
	}
}

func TestIncrementalFailedDumpBreaksChain(t *testing.T) {
	h := newIncrementalHarness(t)
	mustDump(t, h, "d1", nil)

	// CRIU may have cleared the dirty bits before failing, so d1 is no longer a safe parent.
	h.failErr = errors.New("post-dump failed")
	if _, err := h.dump("d2", true, nil); err == nil {
		t.Fatal("expected failure")
	}
	h.failErr = nil

	mustDump(t, h, "d3", nil)
	if h.seen.ParentImg != nil {
		t.Fatalf("dump after a failed one got parent %q, want full", h.seen.GetParentImg())
	}
}

func TestIncrementalNonTrackingDumpKeepsChain(t *testing.T) {
	h := newIncrementalHarness(t)
	mustDump(t, h, "d1", nil)

	// A dump without track_mem leaves the dirty bits alone, so d1 stays the parent.
	if _, err := h.dump("plain", false, nil); err != nil {
		t.Fatal(err)
	}
	if h.seen.ParentImg != nil {
		t.Fatal("non-tracking dump must not get a parent")
	}

	mustDump(t, h, "d2", nil)
	if got := h.seen.GetParentImg(); got != "../d1" {
		t.Fatalf("parent = %q, want ../d1", got)
	}
}

func TestIncrementalOtherProcessGetsNoParent(t *testing.T) {
	h := newIncrementalHarness(t)
	mustDump(t, h, "d1", nil)

	h.start++ // same PID, different process (reuse, or restored)
	mustDump(t, h, "d2", nil)
	if h.seen.ParentImg != nil {
		t.Fatalf("different process got parent %q", h.seen.GetParentImg())
	}
}

func TestIncrementalCompressedDumpIsFullAndNotLinkable(t *testing.T) {
	h := newIncrementalHarness(t)
	mustDump(t, h, "d1", nil)

	gzip := func(req *daemon.DumpReq) { req.Compression = "gzip" }
	resp, err := h.dump("d2", true, gzip)
	if err != nil {
		t.Fatal(err)
	}
	if h.seen.ParentImg != nil {
		t.Fatal("compressed dump must not link to a parent")
	}
	if !h.seen.GetTrackMem() {
		t.Fatal("compressed dump should still track memory")
	}
	if len(resp.Messages) != 0 {
		t.Fatalf("unexpected messages: %v", resp.Messages)
	}

	// d2 cleared the bits but can't be linked to, and d1 is stale: next is full.
	mustDump(t, h, "d3", nil)
	if h.seen.ParentImg != nil {
		t.Fatalf("parent = %q, want none", h.seen.GetParentImg())
	}
}

func TestIncrementalMissingParentFallsBackToFull(t *testing.T) {
	h := newIncrementalHarness(t)
	mustDump(t, h, "d1", nil)
	if err := os.RemoveAll(filepath.Join(h.root, "d1")); err != nil {
		t.Fatal(err)
	}

	mustDump(t, h, "d2", nil)
	if h.seen.ParentImg != nil {
		t.Fatalf("parent = %q, want none (deleted)", h.seen.GetParentImg())
	}
}

func TestIncrementalExplicitParent(t *testing.T) {
	h := newIncrementalHarness(t)
	h.start = 7 // no chain for this process

	base := filepath.Join(h.root, "base")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, criuInventoryImage), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	mustDump(t, h, "d1", func(req *daemon.DumpReq) { req.Criu.ParentImg = proto.String(base) })
	if got := h.seen.GetParentImg(); got != "../base" {
		t.Fatalf("parent = %q, want ../base", got)
	}

	_, err := h.dump("d2", true, func(req *daemon.DumpReq) {
		req.Criu.ParentImg = proto.String(filepath.Join(h.root, "nope"))
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("explicit bad parent: err = %v, want InvalidArgument", err)
	}
}

func TestIncrementalUnsupportedKernelDumpsFull(t *testing.T) {
	h := newIncrementalHarness(t)
	memTrackSupported = func(context.Context, *criu.Criu) (bool, error) { return false, nil }

	resp, err := h.dump("d1", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.seen.TrackMem != nil || h.seen.ParentImg != nil {
		t.Fatalf("track_mem/parent should be cleared, got %v/%q", h.seen.TrackMem, h.seen.GetParentImg())
	}
	if len(resp.Messages) == 0 {
		t.Fatal("expected a message saying the dump was full")
	}
}

// A tracked dump of a child clears the child's bits, so the tree's chain (whose last dump
// covered the child) must not be used as the parent of the next tree dump, and vice versa.
func TestIncrementalOverlappingDumpsBreakChains(t *testing.T) {
	h := newIncrementalHarness(t)
	root, rootStart := h.pid, h.start
	child := &daemon.ProcessState{PID: 5000, StartTime: 2000}
	h.children = []*daemon.ProcessState{{PID: 4999, StartTime: 1999, Children: []*daemon.ProcessState{child}}}

	mustDump(t, h, "tree1", nil)

	// Dump just the grandchild
	h.pid, h.start, h.children = child.PID, child.StartTime, nil
	mustDump(t, h, "child1", nil)

	// The tree's chain is broken: full dump
	h.pid, h.start = root, rootStart
	h.children = []*daemon.ProcessState{{PID: 4999, StartTime: 1999, Children: []*daemon.ProcessState{child}}}
	mustDump(t, h, "tree2", nil)
	if h.seen.ParentImg != nil {
		t.Fatalf("tree dump after a child dump got parent %q, want full", h.seen.GetParentImg())
	}

	// ...and that tree dump broke the child's chain
	h.pid, h.start, h.children = child.PID, child.StartTime, nil
	mustDump(t, h, "child2", nil)
	if h.seen.ParentImg != nil {
		t.Fatalf("child dump after a tree dump got parent %q, want full", h.seen.GetParentImg())
	}
}

func TestIncrementalUnrelatedChainSurvives(t *testing.T) {
	h := newIncrementalHarness(t)
	mustDump(t, h, "a1", nil)

	other := *h
	other.pid, other.start = 7777, 1
	mustDump(t, &other, "b1", nil)

	mustDump(t, h, "a2", nil)
	if got := h.seen.GetParentImg(); got != "../a1" {
		t.Fatalf("parent = %q, want ../a1 (another process's dump must not break the chain)", got)
	}
}

// A relative images directory (e.g. --dir .) must still chain.
func TestIncrementalRelativeImagesDir(t *testing.T) {
	h := newIncrementalHarness(t)
	t.Chdir(h.root)

	rel := func(name string) func(*daemon.DumpReq) {
		return func(req *daemon.DumpReq) { req.Criu.ImagesDir = proto.String(filepath.Join("ckpt", name)) }
	}
	for _, name := range []string{"r1", "r2"} {
		if err := os.MkdirAll(filepath.Join("ckpt", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustDump(t, h, "ckpt/r1", rel("r1"))
	mustDump(t, h, "ckpt/r2", rel("r2"))
	if got := h.seen.GetParentImg(); got != "../r1" {
		t.Fatalf("parent = %q, want ../r1", got)
	}
}
