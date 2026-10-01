package profiling

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/bits"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/keys"
)

type Reference struct {
	Duration time.Duration
	Source   string
	Samples  int
	Key      string
}

const (
	learnedReferenceMinSamples = 5
	learnedReferenceMaxSamples = 8
)

type learnedReferenceHistory struct {
	mu      sync.Mutex
	samples map[string][]int64
}

var learnedReferences = learnedReferenceHistory{samples: make(map[string][]int64)}

var (
	referencePIDPattern       = regexp.MustCompile(`\b(pid|SlowestPID)=\d+`)
	referenceWorkerTagPattern = regexp.MustCompile(`, (fastest|slowest)`)
)

func (data *Data) AddIO(n int64) {
	if data != nil {
		data.IO += n
	}
}

// SetReferenceDuration records a controller-derived phase reference. A zero duration leaves the
// profile unchanged.
func SetReferenceDuration(ctx context.Context, duration time.Duration) {
	SetReference(ctx, Reference{Duration: duration})
}

func SetReference(ctx context.Context, reference Reference) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok || data == nil || reference.Duration <= 0 {
		return
	}
	data.ReferenceDuration = int64(reference.Duration)
	data.ReferenceSource = reference.Source
	data.ReferenceSamples = reference.Samples
	data.ReferenceKey = reference.Key
}

// ApplyLearnedReferences fills reference durations for successful dump and restore rows that
// do not already have a modeled or GPU-specific reference.
func ApplyLearnedReferences(data *Data, operation string) {
	if data == nil || (operation != "dump" && operation != "restore") {
		return
	}

	profileKey := learnedReferenceProfileKey(data)
	for _, component := range data.Components {
		if component == nil || component.Name == "" || component.Duration <= 0 || component.ReferenceDuration > 0 {
			continue
		}

		key := fmt.Sprintf("%s|profile=%s|%s|io=%d", operation, profileKey, normalizeReferenceName(component.Name), component.IO)
		duration, samples := learnedReferences.observe(key, component.Duration)
		component.ReferenceDuration = duration
		component.ReferenceSamples = samples
		component.ReferenceKey = key
		if samples < learnedReferenceMinSamples {
			component.ReferenceSource = "best so far"
		} else {
			component.ReferenceSource = "learned"
		}
	}
}

// Include every row, including modeled rows, so zero-I/O steps learn within a similar workload.
func learnedReferenceProfileKey(data *Data) string {
	rows := make([]string, 0, len(data.Components))
	for _, component := range data.Components {
		if component == nil || component.Name == "" {
			continue
		}
		bucket := 0
		if component.IO > 0 {
			bucket = bits.Len64(uint64(component.IO))
		}
		rows = append(rows, fmt.Sprintf("%s|io=%d", normalizeReferenceName(component.Name), bucket))
	}
	sort.Strings(rows)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(rows, "\n"))))
}

func (history *learnedReferenceHistory) observe(key string, duration int64) (int64, int) {
	history.mu.Lock()
	defer history.mu.Unlock()

	samples := append(history.samples[key], duration)
	if len(samples) > learnedReferenceMaxSamples {
		samples = samples[len(samples)-learnedReferenceMaxSamples:]
	}
	history.samples[key] = samples

	ordered := append([]int64(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	if len(ordered) < learnedReferenceMinSamples {
		return ordered[0], len(samples)
	}
	return ordered[(len(ordered)-1)/4], len(samples)
}

func normalizeReferenceName(name string) string {
	name = referencePIDPattern.ReplaceAllString(name, "$1=*")
	name = referenceWorkerTagPattern.ReplaceAllString(name, "")
	return name
}
