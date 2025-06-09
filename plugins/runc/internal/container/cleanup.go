package container

import (
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/opencontainers/runc/libcontainer"
)

// Implements the cleanup handler for the runc plugin

func Cleanup(details *daemon.Details) (err error) {
	runc := details.GetRunc()
	if runc == nil {
		return fmt.Errorf("missing runc details in cleanup request")
	}
	root := runc.GetRoot()
	id := runc.GetID()

	container, err := libcontainer.Load(root, id)
	if err != nil {
		return fmt.Errorf("failed to load runc container: %w", err)
	}

	return container.Destroy()
}
