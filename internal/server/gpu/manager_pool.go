package gpu

// Implements a GPU manager that manages a pool of GPU controllers.
// Keeps a certain number of controllers warm in the pool, and spins up new ones
// as needed.

type PoolManager struct {
	ManagerMissing
}

func NewPoolManager(size int) *PoolManager {
	return &PoolManager{}
}
