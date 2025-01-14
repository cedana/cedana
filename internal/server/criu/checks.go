package criu

// Health checks for CRIU

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
)

const CRIU_MIN_VERSION = 30000

// CheckVersion checks the installed CRIU version, and if it's compatible
func CheckVersion(manager plugins.Manager) types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		c := criu.MakeCriu()

		component := &daemon.HealthCheckComponent{Name: "version"}

		// Check if CRIU plugin is installed, then use that binary
		var p *plugins.Plugin
		installed := true
		if p = manager.Get("criu"); p.Status != plugins.Installed {
			// Set custom path if specified in config, as a fallback
			if custom_path := config.Global.CRIU.BinaryPath; custom_path != "" {
				component.Warnings = append(component.Warnings,
					"CRIU plugin not installed but a custom CRIU path was provided. It's recommended to install the plugin for full feature support.",
				)
				c.SetCriuPath(custom_path)
			} else if path, err := exec.LookPath("criu"); err == nil {
				component.Warnings = append(component.Warnings,
					"CRIU plugin not installed but CRIU binary found in PATH. It's recommended to install the plugin for full feature support.",
				)
				c.SetCriuPath(path)
			} else {
				installed = false
				component.Errors = append(component.Errors,
					"CRIU plugin is not installed. This is required for userspace C/R support.",
				)
			}
		} else {
			c.SetCriuPath(p.Binaries[0].Name)
		}

		if installed {
			version, err := c.GetCriuVersion(ctx)
			if err == nil {
				component.Data = strconv.Itoa(version)
				if version < CRIU_MIN_VERSION {
					component.Errors = append(component.Errors,
						fmt.Sprintf("Version %d is not supported. Minimum supported is %d", version, CRIU_MIN_VERSION),
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

// CheckFeatures runs the CRIU check command to check for supported/missing features.
// If `all` is true, it will run the check with the `--all` flag, which will include
// extra, experimental, and often unneeded features.
func CheckFeatures(manager plugins.Manager, all bool) types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		c := criu.MakeCriu()

		component := &daemon.HealthCheckComponent{Name: "features"}

		// Check if CRIU plugin is installed, then use that binary
		var p *plugins.Plugin
		installed := true
		if p = manager.Get("criu"); p.Status != plugins.Installed {
			// Set custom path if specified in config, as a fallback
			if custom_path := config.Global.CRIU.BinaryPath; custom_path != "" {
				c.SetCriuPath(custom_path)
			} else if path, err := exec.LookPath("criu"); err == nil {
				c.SetCriuPath(path)
			} else {
				installed = false
			}
		} else {
			c.SetCriuPath(p.Binaries[0].Name)
		}

		if installed {
			var flags []string
			if all {
				flags = append(flags, "--all")
			}
			out, err := c.Check(ctx, flags...)
			warnings, errors := parseCheckOutput(out)
			component.Warnings = append(component.Warnings, warnings...)
			component.Errors = append(component.Errors, errors...)
			if err == nil {
				component.Data = "available"
			} else {
				if len(component.Errors) == 0 {
					component.Data = "available"
					component.Warnings = append(component.Warnings, "Not ideal, but no red flags either")
				} else {
					component.Data = "unavailable"
				}
			}

			return []*daemon.HealthCheckComponent{component}
		}

		return nil
	}
}

// Check if the installed CRIU version is compatible with the opts
func CheckOpts(ctx context.Context, criuInstance *criu.Criu, opts *criu_proto.CriuOpts) error {
	version, err := criuInstance.GetCriuVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get CRIU version: %v", err)
	}

	if version < CRIU_MIN_VERSION {
		return fmt.Errorf("CRIU version %d is not supported. Minimum supported is %d", version, CRIU_MIN_VERSION)
	}

	if opts.LsmProfile != nil {
		if version < 31600 {
			return fmt.Errorf("CRIU version %d does not support LSM profile", version)
		}
	}

	if opts.LsmMountContext != nil {
		if version < 31600 {
			return fmt.Errorf("CRIU version %d does not support LSM mount context", version)
		}
	}

	return nil
}

// Certain CRIU options are not compatible with GPU support.
func CheckOptsGPU(opts *criu_proto.CriuOpts) error {
	if opts.GetLeaveRunning() {
		return fmt.Errorf("Leave running is not compatible with GPU support, yet")
	}
	return nil
}

//////////////////////////
//// Helper functions ////
//////////////////////////

func parseCheckOutput(out string) (warnings, errors []string) {
	// output is multiple lines, either starting with 'Warn' or 'Error'
	// other lines are ignored. must return a list of warnings and errors.

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Warn") {
			warnings = append(warnings, strings.TrimSpace(strings.TrimPrefix(line, "Warn")))
		} else if strings.HasPrefix(line, "Error") {
			errors = append(errors, strings.TrimSpace(strings.TrimPrefix(line, "Error")))
		}
	}

	return
}
