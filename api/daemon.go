package api

import (
	"encoding/json"
	"net"
	"net/rpc"
	"os"
	"strings"

	container "github.com/cedana/cedana/container"
	cedana "github.com/cedana/cedana/types"
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

type RuncRestoreArgs struct {
	ContainerId string
	ImagePath   string
	Opts        *container.RuncOpts
}

type RuncRestoreResp struct {
	Error error
}

type RuncDumpArgs struct {
	WorkPath       string
	Root           string
	CheckpointPath string
	ContainerId    string
	CriuOpts       container.CriuOpts
}

type RuncDumpResp struct {
	Error error
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
	Error error
}

type ContainerDumpResp struct {
	CheckpointPath string
	Error          error
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
	ID   string
	Task string
}

type StartTaskResp struct {
	PID   int32
	Error error
}

type RegisterProcessArgs struct {
	PID int32
}

type RegisterProcessResp struct {
}

type StatusArgs struct {
}

type StatusResp struct {
	IDPID    []map[string]string
	PIDState []map[string]cedana.ProcessState
}

func (cd *CedanaDaemon) Dump(args *DumpArgs, resp *DumpResp) error {
	return cd.client.Dump(args.Dir, args.PID)
}

func (cd *CedanaDaemon) ContainerDump(args *ContainerDumpArgs, resp *ContainerDumpResp) error {
	return cd.client.ContainerDump(args.Ref, args.ContainerId)
}

func (cd *CedanaDaemon) RuncDump(args *RuncDumpArgs, resp *ContainerDumpResp) error {
	return cd.client.RuncDump(args.Root, args.ContainerId, &args.CriuOpts)
}

func (cd *CedanaDaemon) RuncRestore(args *RuncRestoreArgs, resp *RuncRestoreResp) error {
	return cd.client.RuncRestore(args.ImagePath, args.ContainerId, args.Opts)
}

func (cd *CedanaDaemon) Restore(args *RestoreArgs, resp *RestoreResp) error {
	pid, err := cd.client.Restore(args.Path)
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
	go cd.client.startNATSService(args.JobID)

	return nil
}

func (cd *CedanaDaemon) StartTask(args *StartTaskArgs, resp *StartTaskResp) error {
	err := cd.client.TryStartJob(&args.Task, args.ID)
	if err != nil {
		resp.Error = err
	}
	// get pid from ID
	pid, err := cd.client.db.GetPID(args.ID)
	if err != nil {
		resp.Error = err
	}
	resp.PID = pid
	return err
}

func (cd *CedanaDaemon) Ps(args *StatusArgs, resp *StatusResp) error {
	out, err := cd.client.db.ReturnAllEntries()
	if err != nil {
		return err
	}
	for _, entry := range out {
		for k, v := range entry {
			if strings.Contains(v, "{") {
				var state cedana.ProcessState
				err := json.Unmarshal([]byte(v), &state)
				if err != nil {
					return err
				}
				resp.PIDState = append(resp.PIDState, map[string]cedana.ProcessState{
					k: state,
				})
			} else {
				resp.IDPID = append(resp.IDPID, map[string]string{
					k: v,
				})
			}
		}
	}
	return nil
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

	// initialize db here

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
