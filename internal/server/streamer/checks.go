package streamer

// Health checks for streamer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
)

const (
	OPTIMAL_SOFT_LIMIT = 0
	OPTIMAL_HARD_LIMIT = 0
	OPTIMAL_MAX_SIZE   = 4 * utils.MEBIBYTE
)

func Checks(m plugins.Manager) types.Checks {
	return types.Checks{
		Name: "streamer",
		List: []types.Check{
			CheckVersion(m),
			CheckCriu(m),
			CheckKernelSettings(),
		},
	}
}

func CheckVersion(manager plugins.Manager) types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		component := &daemon.HealthCheckComponent{Name: "version"}

		// Check if CRIU plugin is installed, then use that binary
		var p *plugins.Plugin
		if p = manager.Get("streamer"); p.IsInstalled() {
			component.Data = p.Version
		} else {
			component.Errors = append(component.Errors, "Streamer plugin is not installed. This is required for streaming C/R support.")
			component.Data = "unknown"
		}

		return []*daemon.HealthCheckComponent{component}
	}
}

func CheckCriu(manager plugins.Manager) types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		component := &daemon.HealthCheckComponent{Name: "criu"}

		// Check if CRIU plugin is installed
		var p *plugins.Plugin
		if p = manager.Get("criu"); p.IsInstalled() {
			component.Data = "supported"
		} else {
			if custom_path := config.Global.CRIU.BinaryPath; custom_path != "" {
				component.Errors = append(component.Warnings,
					"CRIU plugin not installed but a custom CRIU path was provided. Streaming C/R requires the CRIU plugin.",
				)
				component.Data = "unsupported"
			} else if _, err := exec.LookPath("criu"); err == nil {
				component.Errors = append(component.Warnings,
					"CRIU plugin not installed but CRIU binary found in PATH. Streaming C/R requires the CRIU plugin.",
				)
				component.Data = "unsupported"
			} else {
				component.Errors = append(component.Errors,
					"CRIU plugin is not installed. This is required for userspace C/R support.",
				)
				component.Data = "missing"
			}
		}

		return []*daemon.HealthCheckComponent{component}
	}
}

func CheckKernelSettings() types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		softLimitComponent := &daemon.HealthCheckComponent{Name: "pipe pages soft limit"}
		hardLimitComponent := &daemon.HealthCheckComponent{Name: "pipe pages hard limit"}
		maxSizeComponent := &daemon.HealthCheckComponent{Name: "pipe max size"}

		softLimit, err := os.ReadFile("/proc/sys/fs/pipe-user-pages-soft")
		if err != nil {
			softLimitComponent.Errors = append(softLimitComponent.Errors, "Failed to read /proc/sys/fs/pipe-user-pages-soft")
		} else {
			softLimit = bytes.TrimSpace(softLimit)
			if string(softLimit) == fmt.Sprintf("%d", OPTIMAL_SOFT_LIMIT) {
				softLimitComponent.Data = "unlimited"
			} else {
				softLimitComponent.Warnings = append(softLimitComponent.Warnings, fmt.Sprintf("For optimal performance, `echo %d > /proc/sys/fs/pipe-user-pages-soft`", OPTIMAL_SOFT_LIMIT))
				softLimitComponent.Data = string(softLimit)
			}
		}

		hardLimit, err := os.ReadFile("/proc/sys/fs/pipe-user-pages-hard")
		if err != nil {
			hardLimitComponent.Errors = append(hardLimitComponent.Errors, "Failed to read /proc/sys/fs/pipe-user-pages-hard")
		} else {
			hardLimit = bytes.TrimSpace(hardLimit)
			if string(hardLimit) == fmt.Sprintf("%d", OPTIMAL_HARD_LIMIT) {
				hardLimitComponent.Data = "unlimited"
			} else {
				hardLimitComponent.Warnings = append(hardLimitComponent.Warnings, fmt.Sprintf("For optimal performance, `echo %d > /proc/sys/fs/pipe-user-pages-hard`", OPTIMAL_HARD_LIMIT))
				hardLimitComponent.Data = string(hardLimit)
			}
		}

		maxSize, err := os.ReadFile("/proc/sys/fs/pipe-max-size")
		if err != nil {
			maxSizeComponent.Errors = append(maxSizeComponent.Errors, "Failed to read /proc/sys/fs/pipe-max-size")
		} else {
      maxSize = bytes.TrimSpace(maxSize)
			maxSizeBytes, err := strconv.ParseInt(string(maxSize), 10, 64)
			if err != nil {
				maxSizeComponent.Errors = append(maxSizeComponent.Errors, "Failed to parse /proc/sys/fs/pipe-max-size")
			} else {
				maxSizeComponent.Data = utils.SizeStr(maxSizeBytes)
				if maxSizeBytes < OPTIMAL_MAX_SIZE {
					maxSizeComponent.Warnings = append(maxSizeComponent.Warnings, fmt.Sprintf("For optimal performance, `echo %d > /proc/sys/fs/pipe-max-size`", OPTIMAL_MAX_SIZE))
				}
			}
		}

		return []*daemon.HealthCheckComponent{softLimitComponent, hardLimitComponent, maxSizeComponent}
	}
}
