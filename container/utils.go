package container

import (
	"os"
	"strconv"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	exactArgs = iota
	minArgs
	maxArgs
	specConfig = "config.json"
)

// setupSpec performs initial setup based on the cli.Context for the container
func setupSpec(context *RuncOpts) (*specs.Spec, error) {
	bundle := context.Bundle
	if bundle != "" {
		if err := os.Chdir(bundle); err != nil {
			return nil, err
		}
	}
	spec, err := loadSpec(specConfig)
	if err != nil {
		return nil, err
	}
	return spec, nil
}

// parseBoolOrAuto returns (nil, nil) if s is empty or "auto"
func parseBoolOrAuto(s string) (*bool, error) {
	if s == "" || strings.ToLower(s) == "auto" {
		return nil, nil
	}
	b, err := strconv.ParseBool(s)
	return &b, err
}
