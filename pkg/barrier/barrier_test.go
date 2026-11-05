package barrier

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

func TestAllArrive(t *testing.T) {
	r := NewRegistry()
	const n = 3
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		m, err := r.Join("pod", n, time.Second)
		if err != nil {
			t.Fatalf("join: %v", err)
		}
		wg.Go(func() {
			errs[i] = m.Arrive(context.Background())
			m.Leave(errs[i])
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("member %d: %v", i, err)
		}
	}
	if len(r.groups) != 0 {
		t.Fatalf("group still registered after every member left")
	}
}

func TestArriveWaitsForEveryone(t *testing.T) {
	r := NewRegistry()
	first, _ := r.Join("pod", 2, time.Second)
	second, _ := r.Join("pod", 2, time.Second)

	done := make(chan error, 1)
	go func() { done <- first.Arrive(context.Background()) }()

	select {
	case <-done:
		t.Fatalf("first member got through before the second arrived")
	case <-time.After(50 * time.Millisecond):
	}

	if err := second.Arrive(context.Background()); err != nil {
		t.Fatalf("second: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("first: %v", err)
	}
}

func TestMemberFailingBeforeArrivingFailsGroup(t *testing.T) {
	r := NewRegistry()
	waiting, _ := r.Join("pod", 2, time.Minute)
	failing, _ := r.Join("pod", 2, time.Minute)

	done := make(chan error, 1)
	go func() { done <- waiting.Arrive(context.Background()) }()

	failing.Leave(errors.New("GPU freeze failed"))

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "GPU freeze failed") {
			t.Fatalf("waiting member got %v, want the failing member's error", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("waiting member still waiting after the other failed")
	}
}

func TestLeavingAfterEveryoneArrivedFailsNoOne(t *testing.T) {
	r := NewRegistry()
	a, _ := r.Join("pod", 2, time.Second)
	b, _ := r.Join("pod", 2, time.Second)

	done := make(chan error, 1)
	go func() { done <- a.Arrive(context.Background()) }()
	if err := b.Arrive(context.Background()); err != nil {
		t.Fatalf("b: %v", err)
	}
	b.Leave(errors.New("dump failed after the seize"))
	if err := <-done; err != nil {
		t.Fatalf("a: %v", err)
	}
	a.Leave(nil)
}

func TestTimeout(t *testing.T) {
	r := NewRegistry()
	m, _ := r.Join("pod", 2, 50*time.Millisecond)
	err := m.Arrive(context.Background())
	if err == nil || !strings.Contains(err.Error(), "only 1 of 2") {
		t.Fatalf("got %v, want a timeout", err)
	}
	if len(r.groups) != 0 {
		t.Fatalf("failed group still registered")
	}
}

func TestJoinChecks(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Join("pod", 1, time.Second); err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, err := r.Join("pod", 1, time.Second); err == nil {
		t.Fatalf("joined a full group")
	}
	if _, err := r.Join("pod", 2, time.Second); err == nil {
		t.Fatalf("joined with a different size")
	}
}

func TestMetadataRoundTrip(t *testing.T) {
	out := WithGroup(context.Background(), "action-1", 4)
	md, _ := metadata.FromOutgoingContext(out)
	in := metadata.NewIncomingContext(context.Background(), md)

	group, size, ok, err := FromGroup(in)
	if err != nil || !ok || group != "action-1" || size != 4 {
		t.Fatalf("got %q %d %v %v", group, size, ok, err)
	}

	if _, _, ok, err := FromGroup(context.Background()); ok || err != nil {
		t.Fatalf("plain context: ok=%v err=%v", ok, err)
	}

	bad := metadata.NewIncomingContext(context.Background(), metadata.Pairs(MD_GROUP, "g", MD_SIZE, "zero"))
	if _, _, _, err := FromGroup(bad); err == nil {
		t.Fatalf("accepted an invalid size")
	}
}

func TestArriveAfterFailureFails(t *testing.T) {
	for i := range 200 {
		r := NewRegistry()
		early, _ := r.Join("pod", 2, time.Millisecond)
		late, _ := r.Join("pod", 2, time.Minute)

		if err := early.Arrive(context.Background()); err == nil {
			t.Fatalf("run %d: early member did not time out", i)
		}
		if err := late.Arrive(context.Background()); err == nil {
			t.Fatalf("run %d: late member completed a failed group", i)
		}
	}
}
