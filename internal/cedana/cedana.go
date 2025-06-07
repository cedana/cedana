package cedana

import (
	"context"
	"fmt"
	"sync"

	"github.com/cedana/cedana/internal/cedana/gpu"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/pkg/plugins"
)

// Cedana implements all the capabilities that can be run without a server.
type Cedana struct {
	plugins plugins.Manager
	gpus    gpu.Manager

	wg       *sync.WaitGroup
	lifetime context.Context
	cancel   context.CancelFunc
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

	return &Cedana{
		plugins:  pluginManager,
		gpus:     gpuManager,
		wg:       wg,
		lifetime: ctx,
		cancel:   cancel,
	}, nil
}

func (c *Cedana) Shutdown() {
	c.cancel()

	c.wg.Wait()
}
