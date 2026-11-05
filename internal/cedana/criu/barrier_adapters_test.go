package criu

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/barrier"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/metadata"
)

func inGroup(group string, size int) context.Context {
	md, _ := metadata.FromOutgoingContext(barrier.WithGroup(context.Background(), group, size))
	return metadata.NewIncomingContext(context.Background(), md)
}

func fakeDump(failBeforeSeize error) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (func() <-chan int, error) {
		if failBeforeSeize != nil {
			return nil, failBeforeSeize
		}
		_, err := opts.CRIUCallback.QueryExtFiles(ctx)
		return nil, err
	}
}

func run(ctx context.Context, handler types.Dump) error {
	opts := types.Opts{CRIUCallback: &criu.NotifyCallbackMulti{}}
	_, err := SeizeTogetherForDump(handler)(ctx, opts, &daemon.DumpResp{}, &daemon.DumpReq{})
	return err
}

func TestSeizeTogetherWaitsForGroup(t *testing.T) {
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { errs[i] = run(inGroup("pod-a", 2), fakeDump(nil)) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("dump %d: %v", i, err)
		}
	}
}

func TestSeizeTogetherFailsWhenMemberFails(t *testing.T) {
	waiting := make(chan error, 1)
	go func() { waiting <- run(inGroup("pod-b", 2), fakeDump(nil)) }()

	time.Sleep(20 * time.Millisecond)
	if err := run(inGroup("pod-b", 2), fakeDump(errors.New("validation failed"))); err == nil {
		t.Fatalf("failing dump returned no error")
	}

	select {
	case err := <-waiting:
		if err == nil || !strings.Contains(err.Error(), "validation failed") {
			t.Fatalf("waiting dump got %v, want the other dump's failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("waiting dump still waiting after the other failed")
	}
}

func TestSeizeTogetherWithoutGroup(t *testing.T) {
	if err := run(context.Background(), fakeDump(nil)); err != nil {
		t.Fatalf("dump without a group: %v", err)
	}
	if err := run(inGroup("solo", 1), fakeDump(nil)); err != nil {
		t.Fatalf("dump in a group of one: %v", err)
	}
}
