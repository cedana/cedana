package namespaces

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/spf13/afero"
)

const testMountinfo = `22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw
23 22 0:21 / /proc rw,nosuid shared:12 - proc proc rw
410 22 8:1 /var/spool/slurmd/ns/1234 /var/spool/slurmd/ns/1234 rw,relatime - ext4 /dev/sda1 rw
412 410 0:4 pid:[4026532715] /var/spool/slurmd/ns/1234/.ns/pid rw - nsfs nsfs rw
413 410 0:4 mnt:[4026532713] /var/spool/slurmd/ns/1234/.ns/mnt rw - nsfs nsfs rw
414 22 0:4 net:[4026532800] /run/netns/with\040space rw shared:5 master:2 - nsfs nsfs rw
415 22 0:4 pid:[4026532715] /some/later/1234/duplicate rw - nsfs nsfs rw
416 22 0:4 bogus:[1] /run/1234/bogus rw - nsfs nsfs rw
417 22 0:4 pid:[4026539999] /not/nsfs/1234 rw - tmpfs tmpfs rw
418 22 0:4 user:[4026532900] /var/spool/slurmd/ns/12345/.ns/user rw - nsfs nsfs rw
419 22 0:4 mnt:[4026532901] /var/tmp/slurm-ns/1235/.ns rw - nsfs nsfs rw
garbage
`

func TestPinnedNamespacesFromReader(t *testing.T) {
	// Not the pins of other jobs, nor those of something else entirely (e.g. 'ip netns add')
	tests := []struct {
		jobID    uint32
		expected map[uint64]string
	}{
		{1234, map[uint64]string{
			4026532715: "/var/spool/slurmd/ns/1234/.ns/pid",
			4026532713: "/var/spool/slurmd/ns/1234/.ns/mnt",
		}},
		{1235, map[uint64]string{4026532901: "/var/tmp/slurm-ns/1235/.ns"}}, // job_container/tmpfs
		{123, map[uint64]string{}},
	}

	for _, tt := range tests {
		pins := map[uint64]string{}
		if err := pinnedNamespacesFromReader(strings.NewReader(testMountinfo), tt.jobID, pins); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(pins, tt.expected) {
			t.Errorf("job %d: expected %v, got %v", tt.jobID, tt.expected, pins)
		}
	}
}

func TestParseNsfsRoot(t *testing.T) {
	tests := []struct {
		root  string
		t     configs.NamespaceType
		inode uint64
		ok    bool
	}{
		{"pid:[4026532715]", configs.NEWPID, 4026532715, true},
		{"mnt:[1]", configs.NEWNS, 1, true},
		{"net:[42]", configs.NEWNET, 42, true},
		{"user:[42]", configs.NEWUSER, 42, true},
		{"bogus:[42]", "", 0, false},
		{"pid:[abc]", "", 0, false},
		{"pid:[99999999999999999999999]", "", 0, false},
		{"/", "", 0, false},
		{"", "", 0, false},
	}

	for _, tt := range tests {
		nsType, inode, ok := parseNsfsRoot(tt.root)
		if nsType != tt.t || inode != tt.inode || ok != tt.ok {
			t.Errorf("parseNsfsRoot(%q) = (%v, %d, %v), expected (%v, %d, %v)", tt.root, nsType, inode, ok, tt.t, tt.inode, tt.ok)
		}
	}
}

func TestClassifyNamespaces(t *testing.T) {
	host := map[configs.NamespaceType]uint64{
		configs.NEWNS: 1, configs.NEWPID: 2, configs.NEWUSER: 3, configs.NEWNET: 4,
	}
	pins := map[uint64]string{
		20: "/ns/1234/.ns/pid",
		10: "/ns/1234/.ns/mnt",
		99: "/ns/999/.ns/pid", // stale pin of another job
	}

	t.Run("HostNamespaces", func(t *testing.T) {
		if got := classifyNamespaces(host, host, pins, nil); len(got) != 0 {
			t.Errorf("expected nothing, got %v", got)
		}
	})

	t.Run("Pinned", func(t *testing.T) {
		job := map[configs.NamespaceType]uint64{
			configs.NEWNS: 10, configs.NEWPID: 20, configs.NEWUSER: 3, configs.NEWNET: 4,
		}
		got := classifyNamespaces(job, host, pins, nil)
		found := map[configs.NamespaceType]RecognizedNamespace{}
		for _, ns := range got {
			found[ns.Type] = ns
		}
		expected := map[configs.NamespaceType]RecognizedNamespace{
			configs.NEWNS:  {Type: configs.NEWNS, Inode: 10, Holder: HolderPin, Path: "/ns/1234/.ns/mnt"},
			configs.NEWPID: {Type: configs.NEWPID, Inode: 20, Holder: HolderPin, Path: "/ns/1234/.ns/pid"},
		}
		if !reflect.DeepEqual(found, expected) {
			t.Errorf("expected %v, got %v", expected, found)
		}
	})

	t.Run("PrivateToJob", func(t *testing.T) {
		job := map[configs.NamespaceType]uint64{
			configs.NEWNS: 1, configs.NEWPID: 2, configs.NEWUSER: 3, configs.NEWNET: 40, // unshared, unpinned
		}
		if got := classifyNamespaces(job, host, pins, nil); len(got) != 0 {
			t.Errorf("expected nothing, got %v", got)
		}
	})

	// e.g. a PAM module that unshares the mount namespace in slurmstepd, pinning nothing
	t.Run("HeldByAncestor", func(t *testing.T) {
		job := map[configs.NamespaceType]uint64{
			configs.NEWNS: 50, configs.NEWPID: 2, configs.NEWUSER: 3, configs.NEWNET: 4,
		}
		ancestors := map[configs.NamespaceType]uint32{configs.NEWNS: 4321}
		got := classifyNamespaces(job, host, pins, ancestors)
		expected := []RecognizedNamespace{
			{Type: configs.NEWNS, Inode: 50, Holder: HolderProcess, Path: "/proc/4321/ns/mnt", HolderPID: 4321},
		}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("expected %v, got %v", expected, got)
		}
	})

	t.Run("PinWinsOverAncestor", func(t *testing.T) {
		job := map[configs.NamespaceType]uint64{configs.NEWNS: 10}
		ancestors := map[configs.NamespaceType]uint32{configs.NEWNS: 4321}
		got := classifyNamespaces(job, host, pins, ancestors)
		if len(got) != 1 || got[0].Holder != HolderPin {
			t.Errorf("expected the pin as holder, got %v", got)
		}
	})

	t.Run("AncestorInHostNamespace", func(t *testing.T) {
		ancestors := map[configs.NamespaceType]uint32{configs.NEWNS: 4321}
		if got := classifyNamespaces(host, host, pins, ancestors); len(got) != 0 {
			t.Errorf("expected nothing, got %v", got)
		}
	})
}

// Mount namespace made by unshare(CLONE_NEWNS) + a tmpfs on /var/tmp, as seen by the host and
// by the job. Every mount has a new ID in the copy, only the tmpfs is a different filesystem.
const (
	testHostMountinfo = `22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw
23 22 0:21 / /proc rw,nosuid shared:12 - proc proc rw
24 22 0:22 / /dev/shm rw,nosuid shared:3 - tmpfs tmpfs rw
25 22 8:1 /export/tmp /tmp rw shared:1 - ext4 /dev/sda1 rw
30 22 0:40 / /home rw shared:20 - nfs4 server:/home rw
`
	testJobMountinfo = `522 400 8:1 / / rw,relatime - ext4 /dev/sda1 rw
523 522 0:21 / /proc rw,nosuid - proc proc rw
524 522 0:22 / /dev/shm rw,nosuid - tmpfs tmpfs rw
525 522 8:1 /export/tmp /tmp rw - ext4 /dev/sda1 rw
530 522 0:40 / /home rw - nfs4 server:/home rw
531 522 0:77 / /var/tmp rw,nosuid,nodev,relatime - tmpfs tmpfs rw,size=4096k,mode=1777
`
	// What slurm's namespace plugin does instead: bind mounts of per-job dirs
	testSlurmJobMountinfo = `522 400 8:1 / / rw,relatime - ext4 /dev/sda1 rw
523 522 0:21 / /proc rw,nosuid - proc proc rw
524 522 0:22 / /dev/shm rw,nosuid - tmpfs tmpfs rw
532 524 0:78 / /dev/shm rw - tmpfs tmpfs rw
525 522 8:1 /var/spool/slurmd/ns/1234/.1234/_tmp /tmp rw - ext4 /dev/sda1 rw
530 522 0:40 / /home rw - nfs4 server:/home rw
`
	// A tmpfs of the host's, bind-mounted elsewhere in the job. In full and only a part of it.
	testSharedJobMountinfo = `522 400 8:1 / / rw,relatime - ext4 /dev/sda1 rw
523 522 0:21 / /proc rw,nosuid - proc proc rw
524 522 0:22 / /dev/shm rw,nosuid - tmpfs tmpfs rw
525 522 8:1 /export/tmp /tmp rw - ext4 /dev/sda1 rw
530 522 0:40 / /home rw - nfs4 server:/home rw
531 522 0:22 / /var/tmp rw,nosuid - tmpfs tmpfs rw
532 522 0:22 /scratch /scratch rw,nosuid - tmpfs tmpfs rw
`
)

func TestPrivateMounts(t *testing.T) {
	parse := func(mountinfo string) []mount {
		mounts, err := parseMountinfo(strings.NewReader(mountinfo))
		if err != nil {
			t.Fatal(err)
		}
		return mounts
	}
	host := parse(testHostMountinfo)

	tests := []struct {
		name     string
		job      []mount
		expected []PrivateMount
	}{
		{"HostNamespace", host, nil},
		{"Tmpfs", parse(testJobMountinfo), []PrivateMount{{Mountpoint: "/var/tmp", FSType: "tmpfs"}}},
		{"OvermountAndBind", parse(testSlurmJobMountinfo), []PrivateMount{
			{Mountpoint: "/dev/shm", FSType: "tmpfs"},
			{Mountpoint: "/tmp", FSType: "ext4", OnHost: true},
		}},
		{"SharedWithHost", parse(testSharedJobMountinfo), []PrivateMount{
			{Mountpoint: "/var/tmp", FSType: "tmpfs", OnHost: true},
			{Mountpoint: "/scratch", FSType: "tmpfs", OnHost: true},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := privateMounts(tt.job, host); !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestParseMountinfo(t *testing.T) {
	mounts, err := parseMountinfo(strings.NewReader(testMountinfo))
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 11 {
		t.Fatalf("expected 11 mounts, got %d", len(mounts))
	}
	expected := mount{ID: 414, Device: "0:4", Root: "net:[4026532800]", Mountpoint: "/run/netns/with space", FSType: "nsfs"}
	if mounts[5] != expected {
		t.Errorf("expected %v, got %v", expected, mounts[5])
	}
}

func TestCriuSupportsExternal(t *testing.T) {
	tests := []struct {
		t       configs.NamespaceType
		version int
		ok      bool
	}{
		{configs.NEWNET, 31100, true},
		{configs.NEWNET, 31099, false},
		{configs.NEWPID, 31500, true},
		{configs.NEWPID, 31100, false},
		{configs.NEWNS, 40000, false},
		{configs.NEWUSER, 40000, false},
	}

	for _, tt := range tests {
		if ok, _ := criuSupportsExternal(tt.t, tt.version); ok != tt.ok {
			t.Errorf("criuSupportsExternal(%v, %d) = %v, expected %v", tt.t, tt.version, ok, tt.ok)
		}
	}
}

func TestHandlingFor(t *testing.T) {
	tests := []struct {
		t        configs.NamespaceType
		version  int
		handling Handling
	}{
		{configs.NEWNET, 31100, HandlingExternal},
		{configs.NEWPID, 31500, HandlingExternal},
		{configs.NEWPID, 31100, ""},
		{configs.NEWNS, 30000, HandlingEnter}, // nothing to do with CRIU's version
		{configs.NEWUSER, 40000, ""},
		{configs.NEWIPC, 40000, ""},
	}

	for _, tt := range tests {
		handling, reason := handlingFor(tt.t, tt.version)
		if handling != tt.handling {
			t.Errorf("handlingFor(%v, %d) = %q, expected %q", tt.t, tt.version, handling, tt.handling)
		}
		if (handling == "") != (reason != "") {
			t.Errorf("handlingFor(%v, %d) should give a reason only when there's no handling, got %q", tt.t, tt.version, reason)
		}
	}
}

func TestLoadExternalNamespacesError(t *testing.T) {
	// Not being able to tell is not the same as there being none
	if _, err := loadExternalNamespaces(failingFs{afero.NewMemMapFs()}); err == nil {
		t.Error("expected an error")
	}
}

type failingFs struct{ afero.Fs }

func (failingFs) Open(string) (afero.File, error) { return nil, errors.New("connection reset") }

func TestSaveLoadExternalNamespaces(t *testing.T) {
	fs := afero.NewMemMapFs()

	got, err := loadExternalNamespaces(fs)
	if err != nil || got != nil {
		t.Fatalf("expected nothing from an empty dump, got %v, %v", got, err)
	}

	expected := []ExternalNamespace{
		{Type: configs.NEWPID, Handling: HandlingExternal, Holder: HolderPin},
		{Type: configs.NEWNS, Handling: HandlingEnter, Holder: HolderProcess},
	}
	if err := saveExternalNamespaces(fs, expected); err != nil {
		t.Fatal(err)
	}

	got, err = loadExternalNamespaces(fs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("expected %v, got %v", expected, got)
	}
}

func TestLoadExternalNamespacesFormats(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		expected []ExternalNamespace
		err      bool
	}{
		{"PlainNames", `["pid","net"]`, []ExternalNamespace{
			{Type: configs.NEWPID, Handling: HandlingExternal},
			{Type: configs.NEWNET, Handling: HandlingExternal},
		}, false},
		{"Objects", `[{"type":"mnt","handling":"enter","holder":"process"}]`, []ExternalNamespace{
			{Type: configs.NEWNS, Handling: HandlingEnter, Holder: HolderProcess},
		}, false},
		{"UnknownType", `["bogus"]`, nil, true},
		{"UnknownHandling", `[{"type":"mnt","handling":"teleport"}]`, nil, true},
		{"MissingHandling", `[{"type":"mnt"}]`, nil, true},
		{"Garbage", `{`, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			if err := afero.WriteFile(fs, EXTERNAL_NAMESPACES_FILE, []byte(tt.contents), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := loadExternalNamespaces(fs)
			if (err != nil) != tt.err {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.err && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestUnescapeMountinfo(t *testing.T) {
	tests := map[string]string{
		`/plain`:            "/plain",
		`/with\040space`:    "/with space",
		`/end\040`:          "/end ",
		`/tab\011and\134bs`: "/tab\tand\\bs",
		`/trailing\`:        `/trailing\`,
		`/not\9zzoctal`:     `/not\9zzoctal`,
	}
	for in, expected := range tests {
		if got := unescapeMountinfo(in); got != expected {
			t.Errorf("unescapeMountinfo(%q) = %q, expected %q", in, got, expected)
		}
	}
}
