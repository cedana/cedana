package container

// Helpers for managing runc container state
// Unfortunatly, libcontainer does not expose that interface
// Luckily, it's quite simple

import (
	"os"
	"path/filepath"

	"github.com/opencontainers/runc/libcontainer"
	libcontainer_utils "github.com/opencontainers/runc/libcontainer/utils"
)

const StateFile = "state.json"

func SaveState(root string, id string, state *libcontainer.State) (err error) {
	stateDir := filepath.Join(root, id)

	tmpFile, err := os.CreateTemp(stateDir, "state-")
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
		}
	}()

	err = libcontainer_utils.WriteJSON(tmpFile, state)
	if err != nil {
		return err
	}
	err = tmpFile.Close()
	if err != nil {
		return err
	}

	stateFilePath := filepath.Join(stateDir, StateFile)
	return os.Rename(tmpFile.Name(), stateFilePath)
}
