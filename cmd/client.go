package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	DEFAULT_DUMP_DEADLINE    = 5 * time.Minute
	DEFAULT_RESTORE_DEADLINE = 5 * time.Minute

	CLIENT_CONTEXT_KEY = "client"
)

type Client struct {
	daemonClient daemon.DaemonClient
	conn         *grpc.ClientConn
}

func NewClient(host string, port uint32) (*Client, error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	address := fmt.Sprintf("%s:%d", host, port)
	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, err
	}

	daemonClient := daemon.NewDaemonClient(conn)

	return &Client{
		daemonClient: daemonClient,
		conn:         conn,
	}, nil
}

func (c *Client) Close() {
	c.conn.Close()
}

///////////////////
///// Methods /////
///////////////////

func (c *Client) Dump(ctx context.Context, args *daemon.DumpReq) (*daemon.DumpResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DUMP_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Dump(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) Restore(ctx context.Context, args *daemon.RestoreReq) (*daemon.RestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_RESTORE_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Restore(ctx, args, opts...)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

///////////////////
//    Helpers    //
///////////////////

func getDefaultCallOptions() []grpc.CallOption {
	opts := []grpc.CallOption{}
	if viper.GetBool("cli.wait_for_ready") {
		opts = append(opts, grpc.WaitForReady(true))
	}
	return opts
}
