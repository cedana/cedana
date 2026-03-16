package embedded

import (
	"context"

	"github.com/cedana/cedana/internal/cedana"
)

func New(ctx context.Context, description ...any) (*cedana.Embedded, error) {
	c, err := cedana.NewEmbedded(ctx, description...)
	if err != nil {
		return nil, err
	}

	return (*cedana.Embedded)(c), nil
}
