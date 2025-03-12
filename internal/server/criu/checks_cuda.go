package criu

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
)

const (
	CRIU_MIN_VERSION_FOR_CUDA = 40000
	CRIU_MIN_CUDA_VERSION     = 570
)

func CheckCudaDriverVersion() types.Check {
	return func(ctx context.Context) (res []*daemon.HealthCheckComponent) {
		component := &daemon.HealthCheckComponent{Name: "driver version"}
		res = append(res, component)

		cmd := exec.Command("nvidia-smi")
		output, err := cmd.CombinedOutput()
		if err != nil {
			component.Errors = append(component.Errors, fmt.Sprintf("Failed to run nvidia-smi: %s", err))
			return
		}

		re := regexp.MustCompile(`Driver Version: ([\d.]+)`)
		match := re.FindStringSubmatch(string(output))

		if len(match) < 2 {
			component.Errors = append(component.Errors, "Failed to parse nvidia-smi output")
			return
		}

		version := match[1]

		if len(strings.Split(version, ".")) != 3 {
			component.Errors = append(component.Errors, "Failed to parse nvidia-smi output")
			return
		}

		baseVersionStr := strings.Split(version, ".")[0]

		baseVersion, err := strconv.Atoi(baseVersionStr)
		if err != nil {
			component.Errors = append(component.Errors, fmt.Sprintf("Failed to parse driver version: %s", err))
			return
		}

		component.Data = version

		if baseVersion < CRIU_MIN_CUDA_VERSION {
			component.Errors = append(component.Errors, fmt.Sprintf("Driver version %s is not supported. Minimum supported is %d", version, CRIU_MIN_CUDA_VERSION))
		}

		return
	}
}

func CheckCriuForCuda(manager plugins.Manager) types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		c := criu.MakeCriu()

		component := &daemon.HealthCheckComponent{Name: "criu version"}

		// Check if CRIU plugin is installed, then use that binary
		var p *plugins.Plugin
		installed := true
		if p = manager.Get("criu"); !p.IsInstalled() {
			// Set custom path if specified in config, as a fallback
			if custom_path := config.Global.CRIU.BinaryPath; custom_path != "" {
				c.SetCriuPath(custom_path)
			} else if path, err := exec.LookPath("criu"); err == nil {
				c.SetCriuPath(path)
			} else {
				installed = false
				component.Errors = append(component.Errors,
					"CRIU plugin is not installed. This is required for CUDA C/R.",
				)
			}
		} else {
			c.SetCriuPath(p.Binaries[0].Name)
		}

		if installed {
			version, err := c.GetCriuVersion(ctx)
			if err == nil {
				component.Data = strconv.Itoa(version)
				if version < CRIU_MIN_VERSION_FOR_CUDA {
					component.Errors = append(component.Errors,
						fmt.Sprintf("Version %d is not supported for CUDA C/R. Minimum supported is %d", version, CRIU_MIN_VERSION),
					)
				}
			} else {
				component.Data = "unknown"
				component.Errors = append(component.Errors, fmt.Sprintf("Failed to get version: %v", err))
			}
		}

		return []*daemon.HealthCheckComponent{component}
	}
}
