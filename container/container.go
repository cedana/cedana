package container

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"sync"

	rpc "github.com/checkpoint-restore/go-criu/v6/crit/images"
	"github.com/nravic/cedana/utils"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"golang.org/x/sys/unix"
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
	container_id  string
	m             sync.Mutex
	cgroupManager cgroups.Manager
}

func (c *Container) Checkpoint(extraFiles []*os.File) error {
	logger := utils.GetLogger()
	c.m.Lock()
	defer c.m.Unlock()
	// a container is already running, and has been spawned by someone/something else

	opts := rpc.CriuOpts{}

	if !cgroups.IsCgroup2UnifiedMode() || checkCriuVersion(31400) == nil {
		if fcg := c.cgroupManager.Path("freezer"); fcg != "" {
			opts.FreezeCgroup = proto.String(fcg)
		}
	}

	var t rpc.CriuReqType

	req := &rpc.CriuReq{
		Type: &t,
		Opts: &opts,
	}

	fds, err := unix.Socketpair(unix.AF_LOCAL, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}

	criuClient := os.NewFile(uintptr(fds[0]), "criu-transport-client")
	criuClientFileCon, err := net.FileConn(criuClient)
	criuClient.Close()
	if err != nil {
		return err
	}

	criuClientCon := criuClientFileCon.(*net.UnixConn)
	defer criuClientCon.Close()

	criuServer := os.NewFile(uintptr(fds[1]), "criu-transport-server")
	defer criuServer.Close()

	// check version here

	cmd := exec.Command("criu", "swrk", "3")
	cmd.ExtraFiles = append(cmd.ExtraFiles, criuServer)
	if extraFiles != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, extraFiles...)
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	criuServer.Close()
	criuProcess := cmd.Process

	var criuProcessState *os.ProcessState
	defer func() {
		if criuProcessState == nil {
			_, err := criuProcess.Wait()
			if err != nil {
				logger.Warn().Err(err).Msg("wait on criuProcess returned")
			}
		}
	}()

	data, err := proto.Marshal(req)
	if err != nil {
		logger.Fatal().Err(err)
		return err
	}

	_, err = criuClientCon.Write(data)
	if err != nil {
		logger.Fatal().Err(err).Msg("error writing to criu connection")
	}

	buf := make([]byte, 10*4096)
	oob := make([]byte, 4096)

	for {
		// read messages from client connection
		n, oobn, _, _, err := criuClientCon.ReadMsgUnix(buf, oob)
		// statusFd shenanigans

		if err != nil {
			logger.Fatal().Err(err).Msg("error encountered during CRIU connection")
			return err
		}

		if n == 0 {
			logger.Fatal().Err(errors.New("unexpected EOF"))
		}
		if n == len(buf) {
			logger.Fatal().Err(errors.New("buffer is too small"))
		}

		resp := new(rpc.CriuResp)
		err = proto.Unmarshal(buf[:n], resp)
		if err != nil {
			return err
		}
		if !resp.GetSuccess() {
			typeString := req.GetType().String()
			if typeString == "VERSION" {
				return nil
			}
		}

		t := resp.GetType()
		switch {
		case t == rpc.CriuReqType_DUMP:
		case t == rpc.CriuReqType_RESTORE:
		case t == rpc.CriuReqType_PRE_DUMP:
		default:
			logger.Fatal().Msgf("unable to parse response %s", resp.String())
			return nil
		}

		break
	}

	criuClientCon.CloseWrite()
	_, err = cmd.Process.Wait()
	if err != nil {
		return err
	}

	return nil
}

func (c *Container) criuApplyCgroups(pid int, req *rpc.CriuReq) error {
	if req.GetType() != rpc.CriuReqType_RESTORE {
		return nil
	}

	return nil
}

func Restore() {
	// container is already running and/or has been stopped. Have to intelligently comm w/ manager
}

func checkCriuVersion(version int) error {

	return nil
}
