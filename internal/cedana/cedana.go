package cedana

import (
	"context"
	"fmt"
	"sync"

	"github.com/cedana/cedana/internal/cedana/gpu"
	"github.com/cedana/cedana/internal/version"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/profiling"
)

// Cedana implements all the capabilities that can be run without a server.
type Cedana struct {
	plugins plugins.Manager
	gpus    gpu.Manager

	lifetime context.Context
	cancel   context.CancelFunc

	metricsShutdown   func(context.Context) error
	profilingShutdown func()

	*sync.WaitGroup
}

func New(ctx context.Context) (*Cedana, error) {
	logging.SetLevel(config.Global.LogLevelNoServer)

	wg := &sync.WaitGroup{}
	ctx, cancel := context.WithCancel(ctx)

	pluginManager := plugins.NewLocalManager()

	gpuManager, err := gpu.NewSimpleManager(ctx, wg, pluginManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create GPU manager: %w", err)
	}

	var metricsShutdown func(context.Context) error
	var profilingShutdown func()

	if config.Global.Metrics {
		metricsShutdown, _ = metrics.Init(ctx, version.GetVersion())
	}

	if config.Global.Profiling.Enabled {
		ctx, profilingShutdown = profiling.StartTiming(ctx)
	}

	return &Cedana{
		plugins:           pluginManager,
		gpus:              gpuManager,
		WaitGroup:         wg,
		lifetime:          ctx,
		cancel:            cancel,
		metricsShutdown:   metricsShutdown,
		profilingShutdown: profilingShutdown,
	}, nil
}

func (c *Cedana) Finalize() *profiling.Data {
	data, ok := c.lifetime.Value(keys.PROFILING_CONTEXT_KEY).(*profiling.Data)
	if ok {
		profiling.CleanData(data)
		profiling.FlattenData(data)
		c.profilingShutdown()
	}

	if c.metricsShutdown != nil {
		c.metricsShutdown(c.lifetime)
	}

	c.cancel()

	return data
}
