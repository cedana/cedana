package channel

import (
	"sync"
)

// broadcaster is a type that manages fan-out broadcasting of values to multiple receiver channels.
type broadcaster[T any] struct {
	sync.RWMutex             // protects concurrent access to receivers and closed
	receivers    chan chan T // channel of receiver channels
	closed       bool        // indicates whether broadcasting has ended
}

// Broadcaster returns a function that generates new receiver channels,
// each receiving copies of all values sent on the input channel "in" until it is closed.
func Broadcaster[T any](in <-chan T) func() <-chan T {
	bc := &broadcaster[T]{
		receivers: make(chan chan T, 1024), // buffer size for receiver registration
	}

	// Start fan-out goroutine.
	go func() {
		for val := range in {
			for ch := range bc.receivers {
				select {
				case ch <- val:
				default: // Non-blocking send to avoid blocking the broadcaster
				}
			}
		}
		// Close all receivers when input is closed
		bc.Lock()
		bc.closed = true
		bc.Unlock()

		for ch := range bc.receivers {
			close(ch)
		}
		close(bc.receivers)
	}()

	// Return a function to create a new receiver channel.
	return func() <-chan T {
		bc.Lock()
		defer bc.Unlock()
		if bc.closed {
			ch := make(chan T)
			close(ch)
			return ch
		}
		ch := make(chan T, 16)
		bc.receivers <- ch // Register receiver via channel
		return ch
	}
}
