package channel

import (
	"sync"
)

// broadcaster is a generic type that manages broadcasting values of type T to multiple receivers,
// retaining a buffer of all sent values so new receivers get the entire history upon subscription.
// It protects access with sync.RWMutex, holds all active receiver channels, a buffer of sent values,
// and tracks whether the broadcaster has been closed.
type broadcaster[T any] struct {
	sync.Mutex
	receivers []chan T // active receiver channels
	buffer    []T      // all sent values stored for replay to new receivers
	closed    bool     // tracks whether the broadcaster is closed
}

// Broadcaster returns a function that when called, yields a new channel that receives all values
// ever sent on the input channel 'in'. New channels receive the full history on subscription,
// and receivers added after closure see the full buffer and an immediate close.
// All channel access/updates are internally synchronized.
func Broadcaster[T any](in <-chan T) func() <-chan T {
	bc := &broadcaster[T]{}

	go func() {
		for val := range in {
			bc.Lock()
			// save value to buffer
			bc.buffer = append(bc.buffer, val)
			// send value to all receivers, non-blockingly
			for _, ch := range bc.receivers {
				select {
				case ch <- val:
				default:
				}
			}
			bc.Unlock()
		}
		bc.Lock()
		bc.closed = true
		for _, ch := range bc.receivers {
			close(ch)
		}
		bc.Unlock()
	}()

	return func() <-chan T {
		ch := make(chan T, 1)
		bc.Lock()
		defer bc.Unlock()
		// First, replay all buffered data
		for _, v := range bc.buffer {
			ch <- v
		}
		if bc.closed {
			close(ch)
		} else {
			bc.receivers = append(bc.receivers, ch)
		}
		return ch
	}
}
