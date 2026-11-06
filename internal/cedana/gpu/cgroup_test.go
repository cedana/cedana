package gpu

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

func writeProcs(t *testing.T, dir string, pids ...int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, pid := range pids {
		lines = append(lines, fmt.Sprint(pid))
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup.procs"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCgroupProcsIncludesSubCgroups(t *testing.T) {
	root := t.TempDir()
	writeProcs(t, root, 10, 11, 20)                      // the application, and the controller that joined it
	writeProcs(t, filepath.Join(root, "nested"), 12, 10) // the application's own sub-cgroup

	pids, err := cgroupProcs(root)
	if err != nil {
		t.Fatalf("cgroupProcs: %v", err)
	}
	slices.Sort(pids)
	if !slices.Equal(pids, []int{10, 11, 12, 20}) {
		t.Fatalf("got %v, want [10 11 12 20]", pids)
	}
}

func TestIsController(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	data, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), CONTROLLER_PROCESS_NAME)
	if err := os.WriteFile(bin, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	if !isController(cmd.Process.Pid) {
		t.Fatalf("a process running %s is not recognized as a controller", CONTROLLER_PROCESS_NAME)
	}
	if isController(os.Getpid()) {
		t.Fatalf("the test process is recognized as a controller")
	}
}

func TestCgroupProcsMissingRoot(t *testing.T) {
	if _, err := cgroupProcs(filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Fatalf("no error for a missing cgroup")
	}
}

func TestFreezeWithPtraceSwapsFreezer(t *testing.T) {
	p := &pool{}
	var initialized bool
	callback := &criu_client.NotifyCallback{
		InitializeDumpFunc: func(ctx context.Context, opts *criu_proto.CriuOpts) error {
			initialized = true
			return nil
		},
	}
	p.freezeWithPtrace(callback, "missing", zerolog.Nop())

	opts := &criu_proto.CriuOpts{FreezeCgroup: proto.String("/sys/fs/cgroup/pod/ctr")}
	if err := callback.InitializeDumpFunc(context.Background(), opts); err != nil {
		t.Fatalf("InitializeDump: %v", err)
	}
	if !initialized {
		t.Fatalf("wrapped InitializeDump not called")
	}
	if opts.GetFreezeCgroup() != "" {
		t.Fatalf("FreezeCgroup still set: CRIU would freeze the GPU workers")
	}
}

func TestFreezeWithPtraceHoldsRestOfCgroup(t *testing.T) {
	outside := exec.Command("sh", "-c", "while :; do :; done")
	if err := outside.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		outside.Process.Kill()
		outside.Wait()
	}()

	root := t.TempDir()
	writeProcs(t, root, outside.Process.Pid)

	p := &pool{}
	callback := &criu_client.NotifyCallback{}
	p.freezeWithPtrace(callback, "missing", zerolog.Nop())

	opts := &criu_proto.CriuOpts{FreezeCgroup: proto.String(root)}
	if err := callback.InitializeDumpFunc(context.Background(), opts); err != nil {
		t.Fatalf("InitializeDump: %v", err)
	}
	if _, err := callback.QueryExtFilesFunc(context.Background()); err != nil {
		t.Skipf("cannot ptrace child processes here: %v", err)
	}

	state := func() string {
		data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", outside.Process.Pid))
		return strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))[0]
	}
	if got := state(); got != "t" {
		t.Fatalf("process outside the tree has state %q while dumping, want t (held)", got)
	}

	if err := callback.FinalizeDumpFunc(context.Background(), opts, nil); err != nil {
		t.Fatalf("FinalizeDump: %v", err)
	}
	if got := state(); got == "t" || got == "T" {
		t.Fatalf("process outside the tree has state %q after the dump, want running", got)
	}
}
