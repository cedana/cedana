package containerd_test

import (
	"testing"

	"github.com/cedana/cedana/api/containerd"
)

func TestDumpRootfs(t *testing.T) {
	service := &containerd.ContainerdService{}
	result, err := service.DumpRootfs()

	if err != nil {
		t.Errorf("DumpRootfs() returned an error: %v", err)
	}

	expectedResult := "NOT IMPLEMENTED"
	if result != expectedResult {
		t.Errorf("DumpRootfs() returned %v, expected %v", result, expectedResult)
	}
}
