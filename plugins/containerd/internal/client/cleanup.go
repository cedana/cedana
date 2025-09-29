package client

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
)

// Implements the cleanup handler for the containerd plugin

func Cleanup(ctx context.Context, req *daemon.Details) (err error) {
	details := req.GetContainerd()

	ctx = namespaces.WithNamespace(ctx, details.Namespace)

	client, err := containerd.New(details.Address, containerd.WithDefaultNamespace(details.Namespace))
	if err != nil {
		return fmt.Errorf("failed to create containerd client: %v", err)
	}
	defer client.Close()

	container, err := client.LoadContainer(ctx, details.ID)
	if err != nil {
		return fmt.Errorf("failed to load container %s: %v", details.ID, err)
	}

	task, err := container.Task(ctx, nil)
	if err == nil {
		task.Delete(ctx)
	}

	return container.Delete(ctx, containerd.WithSnapshotCleanup)
}
