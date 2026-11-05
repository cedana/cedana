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

func TestCgroupProcsSkipsControllerChild(t *testing.T) {
	root := t.TempDir()
	writeProcs(t, root, 10, 11)
	writeProcs(t, filepath.Join(root, "nested"), 12, 10)          // the app's own sub-cgroup
	writeProcs(t, filepath.Join(root, CGROUP_CHILD_NAME), 20, 21) // controller and workers

	pids, err := cgroupProcs(root)
	if err != nil {
		t.Fatalf("cgroupProcs: %v", err)
	}
	slices.Sort(pids)
	if !slices.Equal(pids, []int{10, 11, 12}) {
		t.Fatalf("got %v, want [10 11 12]", pids)
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

func TestCreateRestoreCgroups(t *testing.T) {
	mount := t.TempDir()
	old := CGROUPS_MOUNT
	CGROUPS_MOUNT = mount
	t.Cleanup(func() { CGROUPS_MOUNT = old })

	v2 := "/system.slice/kubepods-besteffort-pod1.slice:cri-containerd:abc" // colons, as on k8s
	v1 := "/kubepods/pod1/abc"
	for _, dir := range []string{filepath.Join(mount, v2), filepath.Join(mount, "cpu,cpuacct", v1)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	var initialized int
	callback := &criu_client.NotifyCallback{
		InitializeRestoreFunc: func(ctx context.Context, opts *criu_proto.CriuOpts) error {
			initialized++
			return nil
		},
	}
	createRestoreCgroups(callback, zerolog.Nop())

	opts := &criu_proto.CriuOpts{CgRoot: []*criu_proto.CgroupRoot{
		{Ctrl: proto.String(""), Path: proto.String(v2)},
		{Ctrl: proto.String("cpu,cpuacct"), Path: proto.String(v1)},
		{Ctrl: proto.String("memory"), Path: proto.String("/not/mounted")},
	}}
	for range 2 { // the second time, the children already exist
		if err := callback.InitializeRestoreFunc(context.Background(), opts); err != nil {
			t.Fatalf("InitializeRestore: %v", err)
		}
	}
	if initialized != 2 {
		t.Fatalf("wrapped InitializeRestore called %d times, want 2", initialized)
	}

	for _, dir := range []string{filepath.Join(mount, v2, CGROUP_CHILD_NAME), filepath.Join(mount, "cpu,cpuacct", v1, CGROUP_CHILD_NAME)} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("%s not created: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(mount, "memory")); err == nil {
		t.Fatalf("created a cgroup in a hierarchy that is not mounted")
	}
}
