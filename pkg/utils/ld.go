package utils

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// Utils for checking link dependencies of binaries

type LinkDep struct {
	Name         string
	Path         string // Could be a symlink
	ResolvedPath string // Resolved path of the symlink
}

// GetLinkDeps returns the link dependencies of a binary
// Also resolves any symlinks in the path
func GetLinkDeps(binary string) ([]LinkDep, error) {
	out, err := exec.Command("ldd", binary).Output()
	if err != nil {
		return nil, err
	}

	// Parse output
	deps := make([]LinkDep, 10)

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "=>") {
			parts := strings.Split(line, "=>")
			name := strings.TrimSpace(parts[0])

			part1 := strings.TrimSpace(parts[1])
			path := strings.TrimSpace(strings.Split(part1, " ")[0])

			resolvedPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil, err
			}

			deps = append(deps, LinkDep{name, path, resolvedPath})
		}
	}

	return deps, nil
}
