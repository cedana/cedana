package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"buf.build/gen/go/cedana/cedana/grpc/go/daemon/daemongrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/pkg/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	DEFAULT_DUMP_TIMEOUT    = 5 * time.Minute
	DEFAULT_RESTORE_TIMEOUT = 5 * time.Minute
)

type Client struct {
	daemonClient daemongrpc.DaemonClient
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

	daemonClient := daemongrpc.NewDaemonClient(conn)

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
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DUMP_TIMEOUT)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Dump(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) Restore(
	ctx context.Context,
	args *daemon.RestoreReq,
) (*daemon.RestoreResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_RESTORE_TIMEOUT)
	defer cancel()
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Restore(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) Start(ctx context.Context, args *daemon.StartReq) (*daemon.StartResp, error) {
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Start(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) Manage(ctx context.Context, args *daemon.ManageReq) (*daemon.ManageResp, error) {
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Manage(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) List(ctx context.Context, args *daemon.ListReq) (*daemon.ListResp, error) {
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.List(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) Kill(ctx context.Context, args *daemon.KillReq) (*daemon.KillResp, error) {
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Kill(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) Delete(ctx context.Context, args *daemon.DeleteReq) (*daemon.DeleteResp, error) {
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.Delete(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

// Attach attaches to a managed process/container. Exits the program
// with the exit code of the process.
func (c *Client) Attach(ctx context.Context, args *daemon.AttachReq) error {
	opts := getDefaultCallOptions()
	stream, err := c.daemonClient.Attach(ctx, opts...)
	if err != nil {
		return utils.GRPCErrorColored(err)
	}
	// Send the first start request
	if err := stream.Send(args); err != nil {
		return utils.GRPCErrorColored(err)
	}

	stdIn, stdOut, stdErr, exitCode, errors := utils.NewStreamIOMaster(stream)

	go io.Copy(stdIn, os.Stdin) // since stdin never closes
	outDone := utils.CopyNotify(os.Stdout, stdOut)
	errDone := utils.CopyNotify(os.Stderr, stdErr)
	<-outDone // wait to capture all out
	<-errDone // wait to capture all err

	if err := <-errors; err != nil {
		return utils.GRPCErrorColored(err)
	}

	os.Exit(<-exitCode)

	return nil
}

func (c *Client) ReloadPlugins(ctx context.Context, args *daemon.Empty) (*daemon.Empty, error) {
	opts := getDefaultCallOptions()
	resp, err := c.daemonClient.ReloadPlugins(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

///////////////////
//    Helpers    //
///////////////////

func getDefaultCallOptions() []grpc.CallOption {
	opts := []grpc.CallOption{}
	if config.Get(config.CLI_WAIT_FOR_READY) {
		opts = append(opts, grpc.WaitForReady(true))
	}
	return opts
}
