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
	CedanaManaged bool
	Client        Client        `protobuf:"bytes,1,opt,name=client" json:"client,omitempty"`
	ActionScripts ActionScripts `protobuf:"bytes,2,opt,name=action_scripts,json=actionScripts" json:"action_scripts,omitempty"`
	Connection    Connection    `protobuf:"bytes,3,opt,name=connection" json:"connection,omitempty"`
	SharedStorage SharedStorage `protobuf:"bytes,4,opt,name=shared_storage,json=sharedStorage" json:"shared_storage,omitempty"`
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
		fmt.Printf("No server overrides found..\n")
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
