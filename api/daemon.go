package api

import (
	"net"
	"net/rpc"
	"os"

	"github.com/rs/zerolog"
)

type CedanaDaemon struct {
	client *Client
	logger *zerolog.Logger
	stop   chan struct{}
}

type DumpArgs struct {
	PID int32
	Dir string
}

type ContainerDumpArgs struct {
	Ref         string
	ContainerId string
}

type ContainerRestoreArgs struct {
	ImgPath     string
	ContainerId string
}

type ContainerRestoreResp struct {
	checkpointPath string
	Error          error
}

type ContainerDumpResp struct {
	Error error
}

type DumpResp struct {
	Error error
}

type RestoreArgs struct {
	Path string
}

type RestoreResp struct {
	Error  error
	NewPID int32
}

type ListCheckpointsArgs struct {
}

type ListCheckpointsResp struct {
}

type StartNATSArgs struct {
	SelfID    string
	JobID     string
	AuthToken string
}

type StartNATSResp struct {
	Error error
}

type StartTaskArgs struct {
	Task string
}

type StartTaskResp struct {
	Error error
}

type RegisterProcessArgs struct {
	PID int32
}

type RegisterProcessResp struct {
}

func (cd *CedanaDaemon) Dump(args *DumpArgs, resp *DumpResp) error {
	cd.client.Process.PID = args.PID
	return cd.client.Dump(args.Dir)
}

func (cd *CedanaDaemon) ContainerDump(args *ContainerDumpArgs, resp *ContainerDumpResp) error {
	return cd.client.ContainerDump(args.Ref, args.ContainerId)
}

func (cd *CedanaDaemon) Restore(args *RestoreArgs, resp *RestoreResp) error {
	pid, err := cd.client.Restore(nil, &args.Path)
	if err != nil {
		resp.Error = err
	}
	resp.NewPID = *pid
	return err
}

func (cd *CedanaDaemon) ContainerRestore(args *ContainerRestoreArgs, resp *ContainerRestoreResp) error {
	return cd.client.ContainerRestore(args.ImgPath, args.ContainerId)
}

func (cd *CedanaDaemon) StartNATS(args *StartNATSArgs, resp *StartNATSResp) error {
	// scaffold daemon w/ NATS
	err := cd.client.AddNATS(args.SelfID, args.JobID, args.AuthToken)
	if err != nil {
		resp.Error = err
	}
	go cd.client.startNATSService()

	return nil
}

func (cd *CedanaDaemon) StartTask(args *StartTaskArgs, resp *StartTaskResp) error {
	err := cd.client.TryStartJob(&args.Task)
	if err != nil {
		resp.Error = err
	}
	return err
}

func NewDaemon(stop chan struct{}) *CedanaDaemon {
	c, err := InstantiateClient()
	if err != nil {
		c.logger.Fatal().Err(err).Msg("Could not instantiate client")
	}

	return &CedanaDaemon{
		client: c,
		logger: c.logger,
		stop:   stop,
	}
}

func (cd *CedanaDaemon) Cleanup(listener net.Listener) {
	if listener != nil {
		listener.Close()
	}
	os.Remove("/tmp/cedana.sock")
}

func (cd *CedanaDaemon) StartDaemon() {
	_, err := os.Stat("/tmp/cedana.sock")
	if err == nil {
		cd.logger.Info().Msg("cleaning old socket file...")
		os.Remove("/tmp/cedana.sock")
	}

	listener, err := net.Listen("unix", "/tmp/cedana.sock")
	if err != nil {
		cd.logger.Fatal().Err(err).Msg("could not start daemon")
	}

	defer cd.Cleanup(listener)

L:
	for {
		select {
		case <-cd.stop:
			// this isn't working - fix NR
			// have to kill w/ kill -9 for now
			cd.logger.Info().Msg("stop hit, terminating daemon...")
			break L
		default:
			conn, err := listener.Accept()
			if err != nil {
				cd.logger.Fatal().Err(err).Msg("could not start daemon")
			}
			rpc.ServeConn(conn)
		}
	}
}

func isDaemonRunning() bool {
	_, err := os.Stat("/tmp/cedana.sock")
	return err == nil
}
