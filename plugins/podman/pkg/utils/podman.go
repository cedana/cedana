package utils

// Helpers for talking to podman through its CLI, which avoids depending on
// podman's (rootful) API service being enabled.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const PODMAN_BINARY = "podman"

// Subset of `podman container inspect` output used by this plugin
type Container struct {
	ID         string `json:"Id"`
	Name       string `json:"Name"`
	ImageName  string `json:"ImageName"`
	OCIRuntime string `json:"OCIRuntime"`
	PidFile    string `json:"PidFile"`
	State      struct {
		Status   string `json:"Status"`
		Pid      int    `json:"Pid"`
		ExitCode int    `json:"ExitCode"`
	} `json:"State"`
	Config struct {
		Annotations map[string]string `json:"Annotations"`
	} `json:"Config"`
}

// Runs podman with the given args, returning its stdout
func Podman(ctx context.Context, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, PODMAN_BINARY, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("podman %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Inspects the given containers (by name or ID)
func Inspect(ctx context.Context, ids ...string) ([]*Container, error) {
	out, err := Podman(ctx, append([]string{"container", "inspect", "--format", "json"}, ids...)...)
	if err != nil {
		return nil, err
	}
	containers := []*Container{}
	if err := json.Unmarshal(out, &containers); err != nil {
		return nil, fmt.Errorf("failed to parse podman inspect output: %w", err)
	}
	return containers, nil
}

// Lists the IDs of all containers
func List(ctx context.Context) ([]string, error) {
	out, err := Podman(ctx, "container", "list", "--all", "--no-trunc", "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}
