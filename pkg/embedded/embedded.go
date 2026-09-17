package embedded

import (
	"context"

	"github.com/cedana/cedana/internal/cedana"
)

// Using Cedana as a library does not support plugins. This is a Go limitation
// and Cedana's plugins are built with strict compatibility to the Cedana binary only.
func New(ctx context.Context, description ...any) (*cedana.Cedana, error) {
	c, err := cedana.New(ctx, description...)
	if err != nil {
		return nil, err
	}

	return (*cedana.Cedana)(c), nil
}
