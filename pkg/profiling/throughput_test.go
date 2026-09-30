package profiling

import (
	"context"
	"testing"
	"time"

	"github.com/cedana/cedana/pkg/keys"
)

func TestSetReferenceDuration(t *testing.T) {
	data := &Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)

	SetReferenceDuration(ctx, 25*time.Millisecond)

	if data.ReferenceDuration != int64(25*time.Millisecond) {
		t.Fatalf("reference duration = %s", time.Duration(data.ReferenceDuration))
	}
}
