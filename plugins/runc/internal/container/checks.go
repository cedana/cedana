package container

// Implements server-compatible health checks for runc

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
)

// Checks if the runc binary is available
func CheckBinary() types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		component := &daemon.HealthCheckComponent{Name: "runc binary"}

		// Check if runc binary is available
		if _, err := exec.LookPath("runc"); err != nil {
			component.Data = "missing"
			component.Errors = append(component.Errors, "runc binary not found in PATH")
		} else {
			component.Data = "available"
		}

		return []*daemon.HealthCheckComponent{component}
	}
}

// CheckVersion checks the versions associated with the runc binary
func CheckVersion() types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		components := []*daemon.HealthCheckComponent{}

		if version, err := exec.Command("runc", "--version").Output(); err == nil {
			parts := parseVersion(string(version))
			for _, part := range parts {
				components = append(components, &daemon.HealthCheckComponent{
					Name: part[0],
					Data: part[1],
				})
			}
		} else {
			return []*daemon.HealthCheckComponent{{
				Name:   "runc version",
				Data:   "unknown",
				Errors: []string{fmt.Sprintf("Error getting runc version: %v", err)},
			}}
		}

		return components
	}
}

//////////////////////
// Helper functions //
//////////////////////

// Sample output:
// runc version 1.1.12
// commit: 51d5e94601ceffbbd85688df1c928ecccbfa4685
// spec: 1.0.2-dev
// go: go1.23.3
// libseccomp: 2.5.5
func parseVersion(out string) [][]string {
	lines := strings.Split(out, "\n")
	parts := make([][]string, 0, len(lines))
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		if i == 0 {
			parts = append(parts, []string{"runc version", strings.Split(line, " ")[2]})
			continue
		}
		if strings.Contains(line, "commit") || strings.Contains(line, "go") {
			continue
		}
		part := strings.SplitN(line, ": ", 2)
		part[0] = "runc " + part[0]
		parts = append(parts, part)
	}
	return parts
}
