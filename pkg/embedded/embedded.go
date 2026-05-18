package embedded

import (
	"context"

	"github.com/cedana/cedana/internal/cedana"
)

func New(ctx context.Context, description ...any) (*cedana.Cedana, error) {
	c, err := cedana.New(ctx, description...)
	if err != nil {
		return nil, err
	}

	return (*cedana.Cedana)(c), nil
}
