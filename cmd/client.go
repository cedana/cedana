package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	DEFAULT_DUMP_DEADLINE    = 5 * time.Minute
	DEFAULT_RESTORE_DEADLINE = 5 * time.Minute
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
	if c.conn != nil {
		c.conn.Close()
	}
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
		return nil, handleError(err)
	}
	return resp, nil
}

func (c *Client) Restore(ctx context.Context, args *daemon.RestoreReq) (*daemon.RestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_RESTORE_DEADLINE)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Restore(ctx, args, opts...)
	if err != nil {
		return nil, handleError(err)
	}
	return resp, nil
}

func (c *Client) Start(ctx context.Context, args *daemon.StartReq) (*daemon.StartResp, error) {
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Start(ctx, args, opts...)
	if err != nil {
		return nil, handleError(err)
	}
	return resp, nil
}

func (c *Client) Manage(ctx context.Context, args *daemon.ManageReq) (*daemon.ManageResp, error) {
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Manage(ctx, args, opts...)
	if err != nil {
		return nil, handleError(err)
	}
	return resp, nil
}

func (c *Client) List(ctx context.Context, args *daemon.ListReq) (*daemon.ListResp, error) {
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.List(ctx, args, opts...)
	if err != nil {
		return nil, handleError(err)
	}
	return resp, nil
}

func (c *Client) Kill(ctx context.Context, args *daemon.KillReq) (*daemon.KillResp, error) {
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Kill(ctx, args, opts...)
	if err != nil {
		return nil, handleError(err)
	}
	return resp, nil
}

func (c *Client) Delete(ctx context.Context, args *daemon.DeleteReq) (*daemon.DeleteResp, error) {
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Delete(ctx, args, opts...)
	if err != nil {
		return nil, handleError(err)
	}
	return resp, nil
}

func (c *Client) Attach(ctx context.Context, args *daemon.AttachReq) (daemon.Daemon_AttachClient, error) {
	opts := getDefaultCallOptions()
	stream, err := c.daemonClient.Attach(ctx, opts...)
	if err != nil {
		return nil, err
	}
	// Send the first start request
	if err := stream.Send(args); err != nil {
		return nil, err
	}
	return stream, nil
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

func handleError(err error) error {
	st, ok := status.FromError(err)
	if ok {
		if st.Code() == codes.Unavailable {
			return fmt.Errorf("Daemon unavailable. Is it running?")
		} else {
			return fmt.Errorf("Failed: %v", st.Message())
		}
	}
	return fmt.Errorf("Unknown error: %v", err)
}
