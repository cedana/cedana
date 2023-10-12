package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// types and helper function for managing the server overrides
// dupe of client_config.go in orchestrator, fine for now until we have a shared library

type ConfigClient struct {
	Client        Client        `json:"client"`
	ActionScripts ActionScripts `json:"action_scripts"`
	Connection    Connection    `json:"connection"`
	SharedStorage SharedStorage `json:"shared_storage"`
}

func LoadOverrides(cdir string) (*ConfigClient, error) {
	var serverOverrides ConfigClient

	// load override from file. Fail silently if it doesn't exist, or GenSampleConfig instead
	// overrides are added during instance setup/creation/instantiation (?)
	overridePath := filepath.Join(cdir, "server_overrides.json")
	// do this all in an exists block
	_, err := os.OpenFile(overridePath, 0, 0o644)
	if errors.Is(err, os.ErrNotExist) {
		// do nothing, drop and leave
		return nil, err
	} else {
		f, err := os.ReadFile(overridePath)
		if err != nil {
			fmt.Printf("error reading overrides file: %v", err)
			// couldn't read file :shrug:
			return nil, err
		} else {
			fmt.Printf("found server specified overrides, overriding config...\n")
			err = json.Unmarshal(f, &serverOverrides)
			if err != nil {
				fmt.Printf("some err: %v", err)
				// again, we don't care - drop and leave
				return nil, err
			}
			return &serverOverrides, nil
		}
	}
}
