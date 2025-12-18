package kube

import (
	"context"
	"errors"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"
	"github.com/cedana/cedana/plugins/k8s/internal/defaults"
)

// RuntimeClient is the interface that K8s runtime clients must implement
type RuntimeClient interface {
	String() string
	Pods(context.Context, *k8s.QueryReq) (*k8s.QueryResp, error)
}

// CurrentRuntimeClient returns the current K8s runtime client in use
// on this host.
func CurrentRuntimeClient() (RuntimeClient, error) {
	// TODO: Dynamically determine this from the system.
	// See https://kubernetes.io/docs/tasks/administer-cluster/migrating-from-dockershim/find-out-runtime-you-use/
	runtime := defaults.RUNTIME

	switch runtime {
	case "containerd":
		return NewContainerdClient()
	case "crio":
		return NewCrioClient()
	default:
		return nil, errors.New("unsupported runtime: " + runtime)
	}
}

///////////////
/// Helpers ///
///////////////

// Embed this struct into unimplemented runtime clients
type RuntimeClientUnimplemented struct{}

func (c *RuntimeClientUnimplemented) String() string {
	return "unimplemented"
}

func (c *RuntimeClientUnimplemented) Pods(context.Context, *k8s.QueryReq) (*k8s.QueryResp, error) {
	return nil, errors.New("not implemented")
}
