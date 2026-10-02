package cedana

import (
	"context"
	"fmt"
	"sync"

	"github.com/cedana/cedana/internal/cedana/gpu"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/version"
)

// Cedana implements all the capabilities that can be run without a server.
type Cedana struct {
	plugins plugins.Manager
	gpus    gpu.Manager

	wg       *sync.WaitGroup
	lifetime context.Context
	cancel   context.CancelFunc
}

func New(ctx context.Context, description ...any) (*Cedana, error) {
	logging.SetLevel(config.Global.LogLevelNoServer)
	wg := &sync.WaitGroup{}
	var cancel func()

	if config.Global.Profiling.Enabled {
		ctx, cancel = profiling.StartTiming(ctx, description...)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	if config.Global.Metrics {
		metrics.Init(ctx, wg, "cedana", version.Version)
	}

	pluginManager := plugins.NewLocalManager()

	gpuManager, err := gpu.NewSimpleManager(ctx, wg, pluginManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create GPU manager: %w", err)
	}

	return &Cedana{
		plugins:  pluginManager,
		gpus:     gpuManager,
		wg:       wg,
		lifetime: ctx,
		cancel:   cancel,
	}, nil
}

// Pass the operation only after success to update its reference history.
func (c *Cedana) Finalize(operation ...string) *profiling.Data {
	c.cancel()
	data, ok := c.lifetime.Value(keys.PROFILING_CONTEXT_KEY).(*profiling.Data)
	if ok {
		profiling.Clean(data)
		profiling.Flatten(data)
	}
	c.wg.Wait()
	if len(operation) > 0 {
		profiling.ApplyLearnedReferences(data, operation[0])
	}

	return data
}
