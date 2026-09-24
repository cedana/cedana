package profiling

import (
	"context"
	"testing"
	"time"

	"github.com/cedana/cedana/pkg/keys"
)

func TestSetMinDuration(t *testing.T) {
	data := &Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)

	SetMinDuration(ctx, 25*time.Millisecond)

	if data.MinDuration != int64(25*time.Millisecond) {
		t.Fatalf("min duration = %s", time.Duration(data.MinDuration))
	}
}
