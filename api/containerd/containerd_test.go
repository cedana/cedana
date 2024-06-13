package containerd_test

import (
	"context"
	"testing"

	"github.com/cedana/cedana/api/containerd"
)

func TestDumpRootfs(t *testing.T) {
	service := &containerd.ContainerdService{}
	result, err := service.DumpRootfs(context.Background(), "testid", "test:latest")

	if err != nil {
		t.Errorf("DumpRootfs() returned an error: %v", err)
	}

	expectedResult := "test:latest"
	if result != expectedResult {
		t.Errorf("DumpRootfs() returned %v, expected %v", result, expectedResult)
	}
}
