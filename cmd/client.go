package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"buf.build/gen/go/cedana/cedana/grpc/go/daemon/daemongrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
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
	var address string

	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	address = fmt.Sprintf("%s:%d", host, port)

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

func NewVSOCKClient(contextId uint32, port uint32) (*Client, error) {
	var opts []grpc.DialOption
	var address string

	opts = append(opts,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(utils.VSOCKDialer(contextId, port)),
	)
	address = fmt.Sprintf("vsock://%d:%d", contextId, port)

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

func (c *Client) Dump(ctx context.Context, args *daemon.DumpReq, opts ...grpc.CallOption) (*daemon.DumpResp, *profiling.Data, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DUMP_TIMEOUT)
	defer cancel()
	addDefaultOptions(opts...)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	resp, err := c.daemonClient.Dump(ctx, args, opts...)
	if err != nil {
		return nil, nil, utils.GRPCErrorColored(err)
	}

	data, err := profiling.FromTrailer(trailer)
	if err != nil {
		return nil, nil, err
	}

	return resp, data, nil
}

func (c *Client) Restore(
	ctx context.Context,
	args *daemon.RestoreReq,
	opts ...grpc.CallOption,
) (*daemon.RestoreResp, *profiling.Data, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_RESTORE_TIMEOUT)
	defer cancel()
	addDefaultOptions(opts...)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	resp, err := c.daemonClient.Restore(ctx, args, opts...)
	if err != nil {
		return nil, nil, utils.GRPCErrorColored(err)
	}

	data, err := profiling.FromTrailer(trailer)
	if err != nil {
		return nil, nil, err
	}

	return resp, data, nil
}

func (c *Client) Run(ctx context.Context, args *daemon.RunReq, opts ...grpc.CallOption) (*daemon.RunResp, *profiling.Data, error) {
	addDefaultOptions(opts...)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	resp, err := c.daemonClient.Run(ctx, args, opts...)
	if err != nil {
		return nil, nil, utils.GRPCErrorColored(err)
	}

	data, err := profiling.FromTrailer(trailer)
	if err != nil {
		return nil, nil, err
	}

	return resp, data, nil
}

func (c *Client) Manage(ctx context.Context, args *daemon.RunReq, opts ...grpc.CallOption) (*daemon.RunResp, *profiling.Data, error) {
	addDefaultOptions(opts...)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	resp, err := c.daemonClient.Manage(ctx, args, opts...)
	if err != nil {
		return nil, nil, utils.GRPCErrorColored(err)
	}

	data, err := profiling.FromTrailer(trailer)
	if err != nil {
		return nil, nil, err
	}

	return resp, data, nil
}

func (c *Client) Get(ctx context.Context, args *daemon.GetReq, opts ...grpc.CallOption) (*daemon.GetResp, error) {
	addDefaultOptions(opts...)
	resp, err := c.daemonClient.Get(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) List(ctx context.Context, args *daemon.ListReq, opts ...grpc.CallOption) (*daemon.ListResp, error) {
	addDefaultOptions(opts...)
	resp, err := c.daemonClient.List(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) Kill(ctx context.Context, args *daemon.KillReq, opts ...grpc.CallOption) (*daemon.KillResp, error) {
	addDefaultOptions(opts...)
	resp, err := c.daemonClient.Kill(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) Delete(ctx context.Context, args *daemon.DeleteReq, opts ...grpc.CallOption) (*daemon.DeleteResp, error) {
	addDefaultOptions(opts...)
	resp, err := c.daemonClient.Delete(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

// Attach attaches to a managed process/container. Exits the program
// with the exit code of the process.
func (c *Client) Attach(ctx context.Context, args *daemon.AttachReq, opts ...grpc.CallOption) error {
	addDefaultOptions(opts...)
	stream, err := c.daemonClient.Attach(ctx, opts...)
	if err != nil {
		return utils.GRPCErrorColored(err)
	}
	// Send the first request
	if err := stream.Send(args); err != nil {
		return utils.GRPCErrorColored(err)
	}

	stdIn, stdOut, stdErr, exitCode, errors := cedana_io.NewStreamIOMaster(stream)

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

func (c *Client) GetCheckpoint(ctx context.Context, args *daemon.GetCheckpointReq, opts ...grpc.CallOption) (*daemon.GetCheckpointResp, error) {
	addDefaultOptions(opts...)
	resp, err := c.daemonClient.GetCheckpoint(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) ListCheckpoints(ctx context.Context, args *daemon.ListCheckpointsReq, opts ...grpc.CallOption) (*daemon.ListCheckpointsResp, error) {
	addDefaultOptions(opts...)
	resp, err := c.daemonClient.ListCheckpoints(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}

	return resp, nil
}

func (c *Client) DeleteCheckpoint(ctx context.Context, args *daemon.DeleteCheckpointReq, opts ...grpc.CallOption) (*daemon.DeleteCheckpointResp, error) {
	addDefaultOptions(opts...)
	resp, err := c.daemonClient.DeleteCheckpoint(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) HealthCheck(ctx context.Context, args *daemon.HealthCheckReq, opts ...grpc.CallOption) (*daemon.HealthCheckResp, error) {
	addDefaultOptions(opts...)
	resp, err := c.daemonClient.HealthCheck(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

func (c *Client) ReloadPlugins(ctx context.Context, args *daemon.Empty, opts ...grpc.CallOption) (*daemon.Empty, error) {
	addDefaultOptions(opts...)
	resp, err := c.daemonClient.ReloadPlugins(ctx, args, opts...)
	if err != nil {
		return nil, utils.GRPCErrorColored(err)
	}
	return resp, nil
}

///////////////////
//    Helpers    //
///////////////////

func addDefaultOptions(opts ...grpc.CallOption) {
	if config.Global.CLI.WaitForReady {
		opts = append(opts, grpc.WaitForReady(true))
	}
}
