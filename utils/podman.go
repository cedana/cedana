package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
