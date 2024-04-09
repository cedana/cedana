package crit

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/checkpoint-restore/go-criu/v7/crit"
)

func ReadFds(imageDir string) (map[string]string, error) {
	c := crit.New(nil, nil, imageDir, false, false)

	fds, err := c.ExploreFds()
	if err != nil {
		return nil, fmt.Errorf("failed to explore fds: %w", err)
	}

	result := map[string]string{}

	for _, fd := range fds {
		for _, file := range fd.Files {
			if !strings.HasPrefix(file.Path, "/sys/fs/cgroup/kubepods.slice") ||
				file.Type != "REG" {
				continue
			}
			result[filepath.Base(file.Path)] = file.Path
		}
	}
	return result, nil
}
