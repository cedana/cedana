package cedana

import (
	"context"
	"fmt"
	"sync"

	"github.com/cedana/cedana/internal/cedana/gpu"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/pkg/plugins"
)

// Cedana implements all the capabilities that can be run without a server.
type Cedana struct {
	plugins plugins.Manager
	gpus    gpu.Manager
	db      db.DB

	wg *sync.WaitGroup
}

func New(ctx context.Context) (*Cedana, error) {
	logging.SetLevel(config.Global.LogLevelNoServer)

	var err error

	var database db.DB

	database, err = db.NewSqliteDB(ctx, config.Global.DB.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to create local sqlite db: %w", err)
	}

	if config.Global.DB.Remote {
		database = db.NewPropagatorDB(ctx, config.Global.Connection, database)
	}

	pluginManager := plugins.NewLocalManager()

	gpuManager, err := gpu.NewSimpleManager(ctx, pluginManager, database)
	if err != nil {
		return nil, fmt.Errorf("failed to create GPU manager: %w", err)
	}

	return &Cedana{
		plugins: pluginManager,
		gpus:    gpuManager,
		db:      database,
		wg:      &sync.WaitGroup{},
	}, nil
}
