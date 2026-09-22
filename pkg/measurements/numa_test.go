package measurements

import (
	"context"
	"errors"
	"testing"
)

func TestNUMACalibratorCachesRoute(t *testing.T) {
	calls := 0
	calibrator := newNUMACalibrator(
		func(uint32) (int, int, bool) { return 1, 0, true },
		func(context.Context, int, int, float64, int) (float64, error) {
			calls++
			return 12.5, nil
		},
	)

	for range 2 {
		throughput, resource, ok := calibrator.LimitForPID(context.Background(), 42)
		if !ok || resource != "numa1->numa0" || throughput != 12_500_000_000 {
			t.Fatalf("LimitForPID = (%d, %q, %t)", throughput, resource, ok)
		}
	}
	if calls != 1 {
		t.Fatalf("benchmark calls = %d, want 1", calls)
	}
}

func TestNUMACalibratorRetriesFailedRoute(t *testing.T) {
	calls := 0
	calibrator := newNUMACalibrator(
		func(uint32) (int, int, bool) { return 0, 1, true },
		func(context.Context, int, int, float64, int) (float64, error) {
			calls++
			if calls == 1 {
				return 0, errors.New("numactl unavailable")
			}
			return 8, nil
		},
	)

	if _, _, ok := calibrator.LimitForPID(context.Background(), 42); ok {
		t.Fatal("failed benchmark returned a limit")
	}
	throughput, resource, ok := calibrator.LimitForPID(context.Background(), 42)
	if !ok || resource != "numa0->numa1" || throughput != 8_000_000_000 {
		t.Fatalf("LimitForPID = (%d, %q, %t)", throughput, resource, ok)
	}
	if calls != 2 {
		t.Fatalf("benchmark calls = %d, want 2", calls)
	}
}
