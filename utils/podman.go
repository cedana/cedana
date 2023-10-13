package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	rspec "github.com/opencontainers/runtime-spec/specs-go"
)

func ReadJSONFile(v interface{}, dir, file string) (string, error) {
	file = filepath.Join(dir, file)
	content, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	if err = json.Unmarshal(content, v); err != nil {
		return "", fmt.Errorf("failed to unmarshal %s: %w", file, err)
	}

	return file, nil
}

func WriteJSONFile(v interface{}, dir, file string) (string, error) {
	fileJSON, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshalling JSON: %w", err)
	}
	file = filepath.Join(dir, file)
	if err := os.WriteFile(file, fileJSON, 0o600); err != nil {
		return "", err
	}

	return file, nil
}

func NewFromFile(path string) (*rspec.Spec, envCache, error) {
	cf, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("template configuration at %s not found", path)
		}
		return nil, nil, err
	}
	defer cf.Close()

	return NewFromTemplate(cf)
}

type envCache map[string]int

func NewFromTemplate(r io.Reader) (*rspec.Spec, envCache, error) {
	var config rspec.Spec
	if err := json.NewDecoder(r).Decode(&config); err != nil {
		return nil, nil, err
	}

	envCache := map[string]int{}
	if config.Process != nil {
		envCache = createEnvCacheMap(config.Process.Env)
	}

	return &config, envCache, nil
}

func createEnvCacheMap(env []string) map[string]int {
	envMap := make(map[string]int, len(env))
	for i, val := range env {
		envMap[val] = i
	}
	return envMap
}
