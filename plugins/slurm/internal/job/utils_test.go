package job

import "testing"

const testScope = "/system.slice/slurmstepd.scope"

func TestInJobCgroup(t *testing.T) {
	tests := []struct {
		name string
		path string
		jid  uint32
		want bool
	}{
		{"job ID", testScope + "/job_4/step_batch/user/task_special", 4, true},
		{"job ID of another job", testScope + "/job_5/step_batch/user/task_special", 4, false},
		{"job ID with the same prefix", testScope + "/job_42/step_batch/user/task_special", 4, false},
		{"outside the scope", "/system.slice/slurmd.service", 4, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inJobCgroup(tt.path, tt.jid); got != tt.want {
				t.Fatalf("inJobCgroup(%q, %d) = %v; want %v", tt.path, tt.jid, got, tt.want)
			}
		})
	}
}

func TestJobCgroupDir(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		want   string
		wantOK bool
	}{
		{"SLUID (26.05+)", testScope + "/sFNDM35NQ39R00/step_batch/user/task_special", testScope + "/sFNDM35NQ39R00", true},
		{"job ID", testScope + "/job_4/step_batch/user/task_special", testScope + "/job_4", true},
		{"nested in a container scope", "/system.slice/docker-0123.scope" + testScope + "/sFNDM35NQ39R00/step_0/user/task_0", "/system.slice/docker-0123.scope" + testScope + "/sFNDM35NQ39R00", true},
		{"multiple slurmd", "/system.slice/node1_slurmstepd.scope/sFNDM35NQ39R00/step_batch", "/system.slice/node1_slurmstepd.scope/sFNDM35NQ39R00", true},
		{"scope itself", testScope, "", false},
		{"outside the scope", "/system.slice/slurmd.service", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := jobCgroupDir(tt.path)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("jobCgroupDir(%q) = %q, %v; want %q, %v", tt.path, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestIsSLUID(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"sFNDM35NQ39R00", true},
		{"sEKNKTV3WPV500", true},
		{"job_4", false},
		{"system", false},
		{"sFNDM35NQ39R0", false},  // too short
		{"xFNDM35NQ39R00", false}, // wrong prefix
		{"sFNDM35NQ39RI0", false}, // I is not in Crockford's base32
		{"sfndm35nq39r00", false}, // SLURM prints SLUIDs in upper case
	}
	for _, tt := range tests {
		if got := isSLUID(tt.name); got != tt.want {
			t.Errorf("isSLUID(%q) = %v; want %v", tt.name, got, tt.want)
		}
	}
}

func TestParseSlurmstepdJobID(t *testing.T) {
	tests := []struct {
		name    string
		cmdline string
		want    uint32
		wantOK  bool
	}{
		{"batch step", "slurmstepd: [4.batch]\x00", 4, true},
		{"numbered step", "slurmstepd: [123.0 stepmgr]", 123, true},
		{"idle slurmstepd", "/usr/sbin/slurmstepd infinity\x00", 0, false},
		{"namespace helper", "slurmstepd: [4:namespace]", 0, false},
		{"not slurmstepd", "sleep\x00100\x00", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseSlurmstepdJobID(tt.cmdline)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("parseSlurmstepdJobID(%q) = %d, %v; want %d, %v", tt.cmdline, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
