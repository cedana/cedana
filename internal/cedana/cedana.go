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

	wg *sync.WaitGroup
}

func New(ctx context.Context) (*Cedana, error) {
	logging.SetLevel(config.Global.LogLevelNoServer)

	pluginManager := plugins.NewLocalManager()

	gpuManager, err := gpu.NewSimpleManager(ctx, pluginManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create GPU manager: %w", err)
	}

	return &Cedana{
		plugins: pluginManager,
		gpus:    gpuManager,
		wg:      &sync.WaitGroup{},
	}, nil
}
