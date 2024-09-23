package utils

import (
	"context"
)

// Combine two contexts into one
func CombineContexts(ctx1, ctx2 context.Context) context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	// Combine cancel functions
	go func() {
		select {
		case <-ctx1.Done():
			cancel()
		case <-ctx2.Done():
			cancel()
		}
	}()

	return ctx
}
