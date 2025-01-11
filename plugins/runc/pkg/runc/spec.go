package runc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const SpecConfigFile = "config.json"

// loadSpec loads the specification from the provided path.
func LoadSpec(cPath string) (spec *specs.Spec, err error) {
	cf, err := os.Open(cPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("JSON specification file %s not found", cPath)
		}
		return nil, err
	}
	defer cf.Close()

	if err = json.NewDecoder(cf).Decode(&spec); err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, errors.New("config cannot be null")
	}
	return spec, validateProcessSpec(spec.Process)
}

func CreateLibContainerRlimit(rlimit specs.POSIXRlimit) (configs.Rlimit, error) {
	rl, err := StrToRlimit(rlimit.Type)
	if err != nil {
		return configs.Rlimit{}, err
	}
	return configs.Rlimit{
		Type: rl,
		Hard: rlimit.Hard,
		Soft: rlimit.Soft,
	}, nil
}

// UpdateSpec updates the spec in the provided path
// and creates a backup of the existing spec
func UpdateSpec(cPath string, spec *specs.Spec) error {
	// Copy existing bundle/config.json to bundle/config.json.bak
	// only if a backup doesn't already exist
	_, err := os.Stat(cPath + ".bak")
	if err != nil {
		err := os.Rename(cPath, cPath+".bak")
		if err != nil {
			return fmt.Errorf("failed to backup spec: %v", err)
		}
	}

	// Write the new spec to bundle/config.json
	newSpecFile, err := os.Create(cPath)
	if err != nil {
		return fmt.Errorf("failed to create new spec file: %v", err)
	}
	err = json.NewEncoder(newSpecFile).Encode(spec)
	if err != nil {
		return fmt.Errorf("failed to write new spec to file: %v", err)
	}

	return nil
}

// RestoreSpec restores the backup of the spec, if a backup exists
func RestoreSpec(cPath string) error {
	_, err := os.Stat(cPath + ".bak")
	if err != nil {
		return errors.New("backup spec does not exist")
	}

	// Restore the backup of the spec
	err = os.Rename(cPath+".bak", cPath)
	if err != nil {
		return fmt.Errorf("failed to restore spec: %v", err)
	}

	return nil
}
