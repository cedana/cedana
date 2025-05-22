package gpu

import (
	"context"
	"sync"
)

// Implements a GPU manager that manages a pool of GPU controllers.
// Keeps a certain number of controllers warm in the pool, and spins up new ones
// as needed.

type PoolManager struct {
	ManagerMissing
}

func NewPoolManager(ctx context.Context, wg *sync.WaitGroup, size int) (*PoolManager, error) {
	return &PoolManager{}, nil
}
