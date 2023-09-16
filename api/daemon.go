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

	for {
		conn, err := listener.Accept()
		if err != nil {
			cd.logger.Fatal().Err(err).Msg("could not start daemon")
		}
		rpc.ServeConn(conn)
	}

	defer cd.Cleanup(listener)
}
