package scripts

// Tests the containerd configuration part of install-plugins.sh (between its BEGIN/END
// CONTAINERD CONFIG markers) against node configs as we find them in the wild, and checks
// with real containerd binaries that the result loads, has the cedana runtime, and keeps
// what was configured before (e.g. the nvidia runtime).
//
// Set CEDANA_TEST_CONTAINERD_BINS to a directory with containerd-<version> binaries
// (e.g. containerd-1.7.36, containerd-2.3.7) to check against them; otherwise only the
// script's own output is checked.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

const shimPath = "/usr/local/bin/cedana-shim-runc-v2"

type containerdFixture struct {
	name string
	// Files to create, relative to the root, with {{ROOT}} replaced by the root
	files map[string]string
	// Highest config version among the files (which containerd versions can load them)
	maxVersion int
	// Runtimes, other than cedana, that the merged config must still have
	keepRuntimes []string
}

var containerdFixtures = []containerdFixture{
	{
		name:       "v2 main config only",
		maxVersion: 2,
		files: map[string]string{
			"etc/containerd/config.toml": `version = 2

[plugins."io.containerd.grpc.v1.cri".containerd]
  default_runtime_name = "runc"
`,
		},
		keepRuntimes: []string{"runc"},
	},
	{
		name:       "v2 main with nvidia conf.d (nvidia-ctk on containerd 1.7)",
		maxVersion: 2,
		files: map[string]string{
			"etc/containerd/config.toml": `version = 2
imports = ["{{ROOT}}/etc/containerd/conf.d/*.toml"]
`,
			"etc/containerd/conf.d/99-nvidia.toml": `version = 2

[plugins."io.containerd.grpc.v1.cri".containerd]
  default_runtime_name = "nvidia"

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia]
  runtime_type = "io.containerd.runc.v2"

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia.options]
  BinaryName = "/usr/bin/nvidia-container-runtime"
`,
		},
		keepRuntimes: []string{"nvidia"},
	},
	{
		name:       "v3 main with nvidia conf.d",
		maxVersion: 3,
		files: map[string]string{
			"etc/containerd/config.toml": `version = 3
imports = ["{{ROOT}}/etc/containerd/conf.d/*.toml"]
`,
			"etc/containerd/conf.d/99-nvidia.toml": `version = 3

[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.nvidia]
  runtime_type = "io.containerd.runc.v2"

[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.nvidia.options]
  BinaryName = "/usr/bin/nvidia-container-runtime"
`,
		},
		keepRuntimes: []string{"nvidia"},
	},
	{
		// The peppypopstar node bootstrap (customer P-51): an unversioned main config with
		// legacy CRI sections, importing a version 4 nvidia file and a version 3 registry file
		name:       "unversioned main with v4 nvidia and v3 conf.d (containerd 2.3)",
		maxVersion: 4,
		files: map[string]string{
			"etc/containerd/config.toml": `imports = ["{{ROOT}}/etc/containerd/conf.d/*.toml"]

[plugins."io.containerd.grpc.v1.cri".containerd]
  snapshotter = "overlayfs"
`,
			"etc/containerd/conf.d/99-nvidia.toml": `version = 4

[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.nvidia]
  runtime_type = "io.containerd.runc.v2"

[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.nvidia.options]
  BinaryName = "/usr/bin/nvidia-container-runtime"
`,
			"etc/containerd/conf.d/zz-cs3-registry.toml": `version = 3

[plugins.'io.containerd.cri.v1.images'.registry]
  config_path = "/etc/containerd/certs.d"
`,
		},
		keepRuntimes: []string{"nvidia"},
	},
	{
		name:       "v3 main without imports",
		maxVersion: 3,
		files: map[string]string{
			"etc/containerd/config.toml": `version = 3

[plugins.'io.containerd.cri.v1.runtime'.containerd]
  default_runtime_name = "runc"
`,
		},
		keepRuntimes: []string{"runc"},
	},
	{
		name:       "v3 main with multiline imports",
		maxVersion: 3,
		files: map[string]string{
			"etc/containerd/config.toml": `version = 3
imports = [
  "{{ROOT}}/etc/containerd/other/*.toml",
]
`,
			"etc/containerd/other/10-runc.toml": `version = 3

[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"
`,
		},
		keepRuntimes: []string{"runc"},
	},
}

// The containerd versions that load configs of each version
var containerdVersionsFor = map[int][]string{
	2: {"1.7", "2.0", "2.1", "2.2", "2.3", "2.4"},
	3: {"2.0", "2.1", "2.2", "2.3", "2.4"},
	4: {"2.3", "2.4"},
}

func TestContainerdConfig(t *testing.T) {
	block := containerdConfigBlock(t)
	bins := containerdBinaries(t)

	for _, f := range containerdFixtures {
		t.Run(f.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, f.files)
			config := filepath.Join(root, "etc/containerd/config.toml")
			env := []string{"CONTAINERD_CONFIG_PATH=" + config}

			runContainerdConfig(t, block, root, env)
			after := snapshot(t, root)

			// Idempotent: running again (e.g. a helper restart) changes nothing
			runContainerdConfig(t, block, root, env)
			if again := snapshot(t, root); !equalSnapshots(after, again) {
				t.Errorf("a second run changed the config:\n%s", diffSnapshots(after, again))
			}

			checked := 0
			for _, minor := range containerdVersionsFor[f.maxVersion] {
				for _, bin := range bins[minor] {
					checked++
					runtimes := mergedRuntimes(t, bin, config)
					if got := runtimes["cedana"]; got != shimPath {
						t.Errorf("%s: cedana runtime_path is %q, want %q (runtimes: %v)", filepath.Base(bin), got, shimPath, runtimes)
					}
					for _, name := range f.keepRuntimes {
						if _, ok := runtimes[name]; !ok && name != "runc" {
							t.Errorf("%s: runtime %q is gone (runtimes: %v)", filepath.Base(bin), name, runtimes)
						}
					}
				}
			}
			if checked == 0 {
				t.Logf("no containerd binaries for config version %d, only checked the script's output", f.maxVersion)
			}
		})
	}
}

// k3s and RKE2 regenerate their containerd config, so the script extends it through a template
func TestContainerdConfigRancherTemplate(t *testing.T) {
	block := containerdConfigBlock(t)

	cases := []struct {
		name     string
		files    map[string]string
		template string
		key      string
	}{
		{
			name:     "k3s with a generated v3 config",
			files:    map[string]string{"var/lib/rancher/k3s/agent/etc/containerd/config.toml": "version = 3\n"},
			template: "var/lib/rancher/k3s/agent/etc/containerd/config-v3.toml.tmpl",
			key:      `[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes."cedana"]`,
		},
		{
			name:     "k3s with a generated v2 config",
			files:    map[string]string{"var/lib/rancher/k3s/agent/etc/containerd/config.toml": "version = 2\n"},
			template: "var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl",
			key:      `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes."cedana"]`,
		},
		{
			name: "rke2 with an existing v3 template",
			files: map[string]string{
				"var/lib/rancher/rke2/agent/etc/containerd/config.toml":         "version = 3\n",
				"var/lib/rancher/rke2/agent/etc/containerd/config-v3.toml.tmpl": "{{ template \"base\" . }}\n\n# ours\n",
			},
			template: "var/lib/rancher/rke2/agent/etc/containerd/config-v3.toml.tmpl",
			key:      `[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes."cedana"]`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, c.files)

			runContainerdConfig(t, block, root, nil)
			after := snapshot(t, root)
			runContainerdConfig(t, block, root, nil)
			if again := snapshot(t, root); !equalSnapshots(after, again) {
				t.Errorf("a second run changed the template:\n%s", diffSnapshots(after, again))
			}

			template := after[c.template]
			if !strings.Contains(template, `{{ template "base" . }}`) {
				t.Errorf("template does not include the base template:\n%s", template)
			}
			if !strings.Contains(template, c.key) || !strings.Contains(template, shimPath) {
				t.Errorf("template has no cedana runtime (want %s):\n%s", c.key, template)
			}
			if strings.Count(template, "runtimes.\"cedana\"") != 1 {
				t.Errorf("template has the cedana runtime more than once:\n%s", template)
			}
		})
	}
}

func containerdConfigBlock(t *testing.T) string {
	t.Helper()
	start := strings.Index(InstallPlugins, "# BEGIN CONTAINERD CONFIG")
	end := strings.Index(InstallPlugins, "# END CONTAINERD CONFIG")
	if start < 0 || end < start {
		t.Fatal("no BEGIN/END CONTAINERD CONFIG markers in install-plugins.sh")
	}
	return InstallPlugins[start:end]
}

// Binaries found in CEDANA_TEST_CONTAINERD_BINS, by minor version (e.g. "2.3")
func containerdBinaries(t *testing.T) map[string][]string {
	t.Helper()
	dir := os.Getenv("CEDANA_TEST_CONTAINERD_BINS")
	bins := map[string][]string{}
	if dir == "" {
		t.Log("CEDANA_TEST_CONTAINERD_BINS not set, not checking against containerd")
		return bins
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "containerd-*"))
	minorRe := regexp.MustCompile(`containerd-(\d+\.\d+)\.`)
	for _, path := range paths {
		if m := minorRe.FindStringSubmatch(filepath.Base(path)); m != nil {
			bins[m[1]] = append(bins[m[1]], path)
		}
	}
	if len(bins) == 0 {
		t.Fatalf("no containerd-<version> binaries in %s", dir)
	}
	return bins
}

func writeFixture(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.ReplaceAll(content, "{{ROOT}}", root)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runContainerdConfig(t *testing.T, block, root string, env []string) {
	t.Helper()
	cmd := exec.Command("bash", "-c", "set -e\n"+block)
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"RANCHER_ROOT=" + root,
		"CEDANA_PLUGINS_BIN_DIR=" + filepath.Dir(shimPath),
	}, env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("containerd config failed: %v\n%s", err, out.String())
	}
}

// Every file under root, by relative path
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func equalSnapshots(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func diffSnapshots(a, b map[string]string) string {
	var keys []string
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var diff strings.Builder
	for _, k := range keys {
		if a[k] != b[k] {
			fmt.Fprintf(&diff, "--- %s before:\n%s\n--- %s after:\n%s\n", k, a[k], k, b[k])
		}
	}
	return diff.String()
}

// The CRI runtimes of the config as containerd merges it (all imports, migrated to its own
// config version), by name, with their runtime_path
func mergedRuntimes(t *testing.T, bin, config string) map[string]string {
	t.Helper()
	// Only stdout is the config, containerd logs (e.g. deprecation warnings) to stderr
	var stderr bytes.Buffer
	cmd := exec.Command(bin, "-c", config, "config", "dump")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s does not load the config: %v\n%s", filepath.Base(bin), err, stderr.String())
	}
	var dump map[string]any
	if err := toml.Unmarshal(out, &dump); err != nil {
		t.Fatalf("%s: failed to parse config dump: %v", filepath.Base(bin), err)
	}

	plugins, _ := dump["plugins"].(map[string]any)
	runtimes := map[string]string{}
	for _, key := range []string{"io.containerd.cri.v1.runtime", "io.containerd.grpc.v1.cri"} {
		plugin, _ := plugins[key].(map[string]any)
		containerd, _ := plugin["containerd"].(map[string]any)
		entries, _ := containerd["runtimes"].(map[string]any)
		for name, entry := range entries {
			fields, _ := entry.(map[string]any)
			path, _ := fields["runtime_path"].(string)
			runtimes[name] = path
		}
	}
	return runtimes
}
