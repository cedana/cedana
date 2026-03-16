package cedana

import (
	"context"

	"github.com/cedana/cedana/pkg/profiling"
)

// Cedana implements all the capabilities that can be run as an embeddable runtime.
type Embedded Cedana

func NewEmbedded(ctx context.Context, description ...any) (*Embedded, error) {
	c, err := New(ctx, description...)
	if err != nil {
		return nil, err
	}

	return (*Embedded)(c), nil
}

func (r *Embedded) Wait() {
	(*Cedana)(r).Wait()
}

func (r *Embedded) Finalize() *profiling.Data {
	return (*Cedana)(r).Finalize()
}
