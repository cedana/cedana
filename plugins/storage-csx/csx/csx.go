package csx

import (
	"fmt"

	"github.com/rs/zerolog/log"

	"buf.build/gen/go/cedana/cedana/grpc/go/plugins/csx/csxgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func (s *Storage) newCSXClient() (conn *grpc.ClientConn, csxClient csxgrpc.CSXClient, err error) {
	var opts []grpc.DialOption

	opts = append(
		opts,
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4<<20)),
	)
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err = grpc.NewClient(fmt.Sprintf("unix://%s", s.sockAddr), opts...)
	if err != nil {
		log.Err(err).Msg("could not establish connection to CSX")
		return nil, nil, err
	}

	csxClient = csxgrpc.NewCSXClient(conn)
	return
}
