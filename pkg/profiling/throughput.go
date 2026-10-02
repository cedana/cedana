package profiling

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"math/bits"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/keys"
)

const (
	learnedReferenceMinSamples = 5
	learnedReferenceMaxSamples = 8
	learnedReferenceMaxKeys    = 1024
)

type learnedReferenceHistory struct {
	mu      sync.Mutex
	samples map[string][]referenceSample
	keys    []string
}

type referenceSample struct {
	duration int64
	bytes    uint64
}

var learnedReferences = learnedReferenceHistory{samples: make(map[string][]referenceSample)}

var (
	referencePIDPattern       = regexp.MustCompile(`\b(pid|SlowestPID)=\d+`)
	referenceWorkerTagPattern = regexp.MustCompile(`, (fastest|slowest)`)
)

// Bytes are kept locally so an unmodeled GPU phase can learn a size-adjusted reference.
func SetModeledReference(ctx context.Context, duration time.Duration, bytes uint64) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok || data == nil {
		return
	}
	data.referenceBytes = bytes
	if duration <= 0 {
		return
	}
	data.ReferenceDuration = int64(duration)
	data.ReferenceSource = "modeled"
}

// Fill unmodeled rows from prior successful dump/restore timings.
func ApplyLearnedReferences(data *Data, operation string) {
	if data == nil || (operation != "dump" && operation != "restore") {
		return
	}

	profileKey := learnedReferenceProfileKey(data)
	occurrences := make(map[string]int)
	for _, component := range data.Components {
		if component == nil || component.Name == "" {
			continue
		}
		name := normalizeReferenceName(component.Name)
		occurrence := occurrences[name]
		occurrences[name]++
		if component.Duration <= 0 || component.ReferenceDuration > 0 {
			continue
		}

		key := fmt.Sprintf("%s|%s|%s|occurrence=%d|io=%d", operation, profileKey, name, occurrence, referenceSizeBucket(component))
		duration, samples := learnedReferences.observe(key, component.Duration, component.referenceBytes)
		component.ReferenceDuration = duration
		component.ReferenceSamples = samples
		if samples == 0 {
			continue
		}
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
		rows = append(rows, fmt.Sprintf("%s|io=%d", normalizeReferenceName(component.Name), referenceSizeBucket(component)))
	}
	sort.Strings(rows)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(rows, "\n"))))
}

func referenceSizeBucket(data *Data) int {
	if data.referenceBytes > 0 {
		return bits.Len64(data.referenceBytes - 1)
	}
	if data.IO > 0 {
		return bits.Len64(uint64(data.IO))
	}
	return 0
}

func (history *learnedReferenceHistory) observe(key string, duration int64, bytes uint64) (int64, int) {
	history.mu.Lock()
	defer history.mu.Unlock()

	if _, exists := history.samples[key]; !exists {
		if len(history.keys) == learnedReferenceMaxKeys {
			delete(history.samples, history.keys[0])
			history.keys = history.keys[1:]
		}
		history.keys = append(history.keys, key)
	}

	samples := history.samples[key]
	ordered := append([]referenceSample(nil), samples...)
	for i := range ordered {
		if bytes > 0 {
			ordered[i].duration = int64(math.Ceil(float64(bytes) * float64(ordered[i].duration) / float64(ordered[i].bytes)))
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].duration < ordered[j].duration })
	var reference int64
	if len(ordered) > 0 {
		index := 0
		if len(ordered) >= learnedReferenceMinSamples {
			index = (len(ordered) - 1) / 4
		}
		reference = ordered[index].duration
	}

	samples = append(samples, referenceSample{duration: duration, bytes: bytes})
	if len(samples) > learnedReferenceMaxSamples {
		samples = samples[len(samples)-learnedReferenceMaxSamples:]
	}
	history.samples[key] = samples

	return reference, len(ordered)
}

func normalizeReferenceName(name string) string {
	name = referencePIDPattern.ReplaceAllString(name, "$1=*")
	name = referenceWorkerTagPattern.ReplaceAllString(name, "")
	return name
}
