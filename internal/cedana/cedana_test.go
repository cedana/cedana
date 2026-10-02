package cedana

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
)

func TestFinalizeLearnsOnlySuccessfulOperations(t *testing.T) {
	name := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	profile := func() *Cedana {
		data := &profiling.Data{Name: "dump", Duration: 100, Components: []*profiling.Data{
			{Name: name, Duration: 10},
		}}
		return &Cedana{
			lifetime: context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data),
			cancel:   func() {},
			wg:       &sync.WaitGroup{},
		}
	}
	profile().Finalize() // Failed operations finalize without entering history.
	for index := range 2 {
		data := profile().Finalize("dump")
		row := data.Components[0]
		if row.ReferenceSamples != index || (index == 1 && row.ReferenceDuration != 10) {
			t.Fatalf("successful sample %d: %#v", index, row)
		}
	}
	restore := profile().Finalize("restore")
	if restore.Components[0].ReferenceSamples != 0 {
		t.Fatal("restore reused dump history")
	}
}
