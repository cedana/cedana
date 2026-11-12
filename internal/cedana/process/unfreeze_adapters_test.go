package process

import (
	"context"
	"os/exec"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
)

// A process that died while frozen must not stop the unfreeze of what was frozen with it,
// e.g. the container cgroup.
func TestFillProcessStateForUnfreezeProcessGone(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	called := false
	next := func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (func() <-chan int, error) {
		called = true
		return nil, nil
	}

	resp := &daemon.DumpResp{State: &daemon.ProcessState{PID: uint32(cmd.Process.Pid)}}
	if _, err := FillProcessStateForUnfreeze(next)(context.Background(), types.Opts{}, resp, &daemon.DumpReq{}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("unfreeze stopped at a process that is gone")
	}
}
