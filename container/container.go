package container

import (
	"sync"

	"github.com/checkpoint-restore/go-criu/rpc"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"google.golang.org/protobuf/proto"
)

// We use libcontainer here to directly create containers.
// runc has some support for CRIU built in, and we can tack on top of this
// at a lower level, effectively giving Cedana the ability to checkpoint anything that uses runc.

// if a typical user flow is giving us the container and expecting us to manage it's lifecycle...
// scope starts to balloon into container management + checkpointing, and not just checkpointing.
// have to try and decouple that somehow...

// so say the typical flow is podman -> containerd -> runc, we need to intercept it somehow?
// or take info from the podman level and then use it in runc to generate checkpoints...

// so what we really want is to abstract checkpointing away from runc, so that it's usable by any container tool (that's OCI compliant)

// alternatively, "collect" checkpoint/restore APIs across ALL container runtimes. Differences are resolved with PRs into each runtime API

// Another alternative is copying how runc checkpoints containers with CRIU and applying it across the board, modifying as we see fit. 
// Going to go with this for now I think - it's more generalizable to runc and gives us a LOT of flexibility 

type Container struct {
	container_id string
	m            sync.Mutex
}

func getContainer(id string) *Container {
	return &Container{container_id: id}
}

func (c *Container) Checkpoint() {
	c.m.Lock()
	defer c.m.Unlock()
	// a container is already running, and has been spawned by someone/something else
	getContainer("blabla")

	if !cgroups.IsCgroup2UnifiedMode() || checkCriuVersion(31400) == nil {
		if fcg := c.cgroupManager.Path("freezer"); fcg != "" {
			rpcOpts.FreezeCgroup = proto.String(fcg)
		}
	}

	var t rpc.CriuReqType
	var opts rpc.CriuOpts

	req := &rpc.CriuReq{
		Type: &t,
		Opts: &opts,
	}

	err := 

}

func Restore() {
	// container is already running and/or has been stopped. Have to intelligently comm w/ manager
}

func checkCriuVersion(version int) error {

	return nil
}
