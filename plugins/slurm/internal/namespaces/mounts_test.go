package namespaces

import (
	"os"
	"path/filepath"
	"testing"
)

// The mounts of a job under job_container/tmpfs and of init, captured on SLURM 26.05.4
func loadMountinfo(t *testing.T, name string) []mount {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	mounts, err := parseMountinfo(file)
	if err != nil {
		t.Fatal(err)
	}
	return mounts
}

func TestSlurmMountsJobContainerTmpfs(t *testing.T) {
	job := loadMountinfo(t, "job_container_tmpfs-job.mountinfo")
	node := loadMountinfo(t, "job_container_tmpfs-init.mountinfo")

	got := slurmMounts(job, node, 1)
	byPath := map[string]PrivateMount{}
	for _, m := range got {
		byPath[m.Mountpoint] = m
	}
	if len(got) != 2 {
		t.Fatalf("expected /tmp and /dev/shm, got %+v", got)
	}

	tmp, ok := byPath["/tmp"]
	if !ok || tmp.FSType != "overlay" || tmp.Root != "/mnt/nvme/slurm-ns/1/.1/_tmp" {
		t.Errorf("/tmp: got %+v", tmp)
	}
	shm, ok := byPath["/dev/shm"]
	if !ok || shm.FSType != "tmpfs" || shm.Root != "/" {
		t.Errorf("/dev/shm: got %+v", shm)
	}
	// The job's /dev/shm replaces the node's
	for _, m := range node {
		if m.Mountpoint == "/dev/shm" && m.Device == shm.Device {
			t.Errorf("/dev/shm of the job has the node's device %s", m.Device)
		}
	}
}

func TestSlurmMountsNothingOfTheNode(t *testing.T) {
	node := loadMountinfo(t, "job_container_tmpfs-init.mountinfo")

	// A process in init's namespace has init's mounts
	if got := slurmMounts(node, node, 1); len(got) != 0 {
		t.Errorf("expected no private mount, got %+v", got)
	}
}

func TestSlurmMountsByPathAlone(t *testing.T) {
	job := loadMountinfo(t, "job_container_tmpfs-job.mountinfo")

	// Without the node's mounts, the path rule alone
	got := slurmMounts(job, nil, 1)
	if len(got) != 1 || got[0].Mountpoint != "/tmp" {
		t.Errorf("expected /tmp alone, got %+v", got)
	}
	// Another job's path is not this job's
	if got := slurmMounts(job, nil, 2); len(got) != 0 {
		t.Errorf("expected nothing for job 2, got %+v", got)
	}
}

func TestOwnMountNamespaceOf(t *testing.T) {
	job := loadMountinfo(t, "job_container_tmpfs-job.mountinfo")
	node := loadMountinfo(t, "job_container_tmpfs-init.mountinfo")

	own, err := ownMountNamespaceOf(job, node)
	if err != nil || !own {
		t.Errorf("job: expected its own mount namespace, got %v, %v", own, err)
	}
	own, err = ownMountNamespaceOf(node, node)
	if err != nil || own {
		t.Errorf("init: expected init's mount namespace, got %v, %v", own, err)
	}
	if _, err := ownMountNamespaceOf(nil, node); err == nil {
		t.Error("expected an error for a process with no mounts")
	}
}
