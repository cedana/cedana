package barrier

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc/metadata"
)

const (
	MD_GROUP        = "cedana-seize-group"
	MD_SIZE         = "cedana-seize-group-size"
	DEFAULT_TIMEOUT = 2 * time.Minute
)

func WithGroup(ctx context.Context, group string, size int) context.Context {
	return metadata.AppendToOutgoingContext(ctx, MD_GROUP, group, MD_SIZE, strconv.Itoa(size))
}

func FromGroup(ctx context.Context) (group string, size int, ok bool, err error) {
	md, found := metadata.FromIncomingContext(ctx)
	if !found {
		return "", 0, false, nil
	}
	groups, sizes := md.Get(MD_GROUP), md.Get(MD_SIZE)
	if len(groups) == 0 {
		return "", 0, false, nil
	}
	if len(sizes) == 0 {
		return "", 0, false, fmt.Errorf("seize group %q has no size", groups[0])
	}
	size, err = strconv.Atoi(sizes[0])
	if err != nil || size < 1 {
		return "", 0, false, fmt.Errorf("invalid seize group size %q", sizes[0])
	}
	return groups[0], size, true, nil
}

type Registry struct {
	mu     sync.Mutex
	groups map[string]*Barrier
}

var Default = NewRegistry()

func NewRegistry() *Registry {
	return &Registry{groups: map[string]*Barrier{}}
}

type Barrier struct {
	registry *Registry
	group    string
	size     int
	timeout  time.Duration

	mu      sync.Mutex
	joined  int
	arrived int
	left    int
	ready   chan struct{}
	failed  chan struct{}
	err     error
}

type Member struct {
	b       *Barrier
	arrived bool
	left    bool
}

func (r *Registry) Join(group string, size int, timeout time.Duration) (*Member, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	b, ok := r.groups[group]
	if !ok {
		b = &Barrier{
			registry: r,
			group:    group,
			size:     size,
			timeout:  timeout,
			ready:    make(chan struct{}),
			failed:   make(chan struct{}),
		}
		r.groups[group] = b
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.size != size {
		return nil, fmt.Errorf("seize group %q has size %d, not %d", group, b.size, size)
	}
	if b.joined == b.size {
		return nil, fmt.Errorf("seize group %q already has its %d members", group, size)
	}
	b.joined++

	return &Member{b: b}, nil
}

func (m *Member) Arrive(ctx context.Context) error {
	b := m.b
	b.mu.Lock()
	if !m.arrived {
		m.arrived = true
		b.arrived++
		if b.arrived == b.size {
			close(b.ready)
		}
	}
	b.mu.Unlock()

	timer := time.NewTimer(b.timeout)
	defer timer.Stop()

	select {
	case <-b.ready:
		return nil
	case <-b.failed:
		return b.err
	case <-timer.C:
		b.fail(fmt.Errorf("seize group %q: only %d of %d dumps seized their processes within %s", b.group, b.arrivedCount(), b.size, b.timeout))
		return b.err
	case <-ctx.Done():
		b.fail(fmt.Errorf("seize group %q: a dump was cancelled: %w", b.group, ctx.Err()))
		return b.err
	}
}

func (m *Member) Leave(err error) {
	b := m.b
	b.mu.Lock()
	if m.left {
		b.mu.Unlock()
		return
	}
	m.left = true
	b.left++
	done := b.left == b.size
	arrived := m.arrived
	b.mu.Unlock()

	if !arrived {
		if err == nil {
			err = fmt.Errorf("it ended before seizing its processes")
		}
		b.fail(fmt.Errorf("seize group %q: a dump failed: %w", b.group, err))
	}

	if done {
		b.registry.mu.Lock()
		if b.registry.groups[b.group] == b {
			delete(b.registry.groups, b.group)
		}
		b.registry.mu.Unlock()
	}
}

func (b *Barrier) fail(err error) {
	b.mu.Lock()
	select {
	case <-b.ready:
		b.mu.Unlock()
		return
	case <-b.failed:
		b.mu.Unlock()
		return
	default:
	}
	b.err = err
	close(b.failed)
	b.mu.Unlock()

	b.registry.mu.Lock()
	if b.registry.groups[b.group] == b {
		delete(b.registry.groups, b.group)
	}
	b.registry.mu.Unlock()
}

func (b *Barrier) arrivedCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.arrived
}
