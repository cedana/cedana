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
}

type DumpArgs struct {
	PID int32
	Dir string
}

type DumpResp struct {
	Error error
}

type RestoreArgs struct {
	Path string
}

type RestoreResp struct {
	Error error
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

func (cd *CedanaDaemon) Dump(args *DumpArgs, resp *DumpResp) error {
	cd.client.Process.PID = args.PID
	return cd.client.Dump(args.Dir)
}

func (cd *CedanaDaemon) Restore(args *RestoreArgs, resp *RestoreResp) error {
	err := cd.client.Restore(nil, &args.Path)
	if err != nil {
		resp.Error = err
	}
	return err
}

func (cd *CedanaDaemon) StartNATS(args *StartNATSArgs, resp *StartNATSResp) error {
	// scaffold daemon w/ NATS
	err := cd.client.AddNATS(args.SelfID, args.JobID, args.AuthToken)
	if err != nil {
		resp.Error = err
	}
	go cd.client.startNATSService()

 // set up broadcasters, listen and c/r, same way we do with the old daemon

	return nil
}

func NewDaemon() *CedanaDaemon {
	c, err := InstantiateClient()
	if err != nil {
		c.logger.Fatal().Err(err).Msg("Could not instantiate client")
	}

	return &CedanaDaemon{
		client: c,
		logger: c.logger,
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
		os.Remove("/tmp/cedana.sock")
	}

	listener, err := net.Listen("unix", "/tmp/cedana.sock")
	if err != nil {
		cd.logger.Fatal().Err(err).Msg("could not start daemon")
	}

	defer cd.Cleanup(listener)

	for {
		conn, err := listener.Accept()
		if err != nil {
			cd.logger.Fatal().Err(err).Msg("could not start daemon")
		}
		rpc.ServeConn(conn)
	}
}
