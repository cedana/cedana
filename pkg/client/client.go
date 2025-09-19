package client

// A friendly wrapper over the gRPC client to the daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"buf.build/gen/go/cedana/cedana/grpc/go/daemon/daemongrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

const (
	DEFAULT_DUMP_TIMEOUT     = 5 * time.Minute
	DEFAULT_FREEZE_TIMEOUT   = 1 * time.Minute
	DEFAULT_UNFREEZE_TIMEOUT = 1 * time.Minute
	DEFAULT_RESTORE_TIMEOUT  = 5 * time.Minute
	DEFAULT_RUN_TIMEOUT      = 1 * time.Minute
	DEFAULT_MANAGE_TIMEOUT   = 1 * time.Minute
	DEFAULT_DB_TIMEOUT       = 20 * time.Second
	DEFAULT_HEALTH_TIMEOUT   = 1 * time.Minute
)

type Client struct {
	daemonClient daemongrpc.DaemonClient
	*grpc.ClientConn
}

func New(address, protocol string) (*Client, error) {
	var conn *grpc.ClientConn
	var err error
	var opts []grpc.DialOption

	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	protocol = strings.ToLower(protocol)

	switch protocol {
	case "tcp":
		if address == "" {
			address = config.DEFAULT_TCP_ADDR
		}
		conn, err = grpc.NewClient(address, opts...)
	case "unix":
		if address == "" {
			address = config.DEFAULT_SOCK_ADDR
		}
		conn, err = grpc.NewClient(fmt.Sprintf("unix://%s", address), opts...)
	case "vsock":
		if address == "" {
			return nil, fmt.Errorf("address must be provided for vsock")
		}
		if !strings.Contains(address, ":") {
			return nil, fmt.Errorf("address must be in the format 'contextId:port'")
		}
		parts := strings.Split(address, ":")
		contextId, err := strconv.ParseUint(parts[0], 10, 32)
		port, err := strconv.ParseUint(parts[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse vsock address: %w", err)
		}
		opts = append(opts, grpc.WithContextDialer(utils.VSOCKDialer(uint32(contextId), uint32(port))))
		conn, err = grpc.NewClient(fmt.Sprintf("vsock://%d:%d", contextId, port), opts...)
	default:
		err = fmt.Errorf("invalid protocol: %s", protocol)
	}

	if err != nil {
		return nil, err
	}

	daemonClient := daemongrpc.NewDaemonClient(conn)

	return &Client{
		daemonClient: daemonClient,
		ClientConn:   conn,
	}, nil
}

///////////////////
///// Methods /////
///////////////////

func (c *Client) DumpVM(ctx context.Context, args *daemon.DumpVMReq, opts ...grpc.CallOption) (*daemon.DumpVMResp, *profiling.Data, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DUMP_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	resp, err := c.daemonClient.DumpVM(ctx, args, opts...)
	if err != nil {
		return resp, nil, utils.GRPCErrorColored(err)
	}

	data, err := profiling.FromTrailer(trailer)
	if err != nil {
		return resp, nil, err
	}

	return resp, data, nil
}

func (c *Client) Dump(ctx context.Context, args *daemon.DumpReq, opts ...grpc.CallOption) (*daemon.DumpResp, *profiling.Data, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DUMP_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	resp, err := c.daemonClient.Dump(ctx, args, opts...)
	if err != nil {
		return resp, nil, utils.GRPCErrorColored(err)
	}

	data, err := profiling.FromTrailer(trailer)
	if err != nil {
		return resp, nil, err
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
	opts = addDefaultOptions(opts)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	resp, err := c.daemonClient.Restore(ctx, args, opts...)
	if err != nil {
		return resp, nil, utils.GRPCErrorColored(err)
	}

	data, err := profiling.FromTrailer(trailer)
	if err != nil {
		return resp, nil, err
	}

	return resp, data, nil
}

func (c *Client) Freeze(ctx context.Context, args *daemon.DumpReq, opts ...grpc.CallOption) (*daemon.DumpResp, *profiling.Data, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_FREEZE_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	resp, err := c.daemonClient.Freeze(ctx, args, opts...)
	if err != nil {
		return resp, nil, utils.GRPCErrorColored(err)
	}

	data, err := profiling.FromTrailer(trailer)
	if err != nil {
		return resp, nil, err
	}

	return resp, data, nil
}

func (c *Client) Unfreeze(ctx context.Context, args *daemon.DumpReq, opts ...grpc.CallOption) (*daemon.DumpResp, *profiling.Data, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_UNFREEZE_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	resp, err := c.daemonClient.Unfreeze(ctx, args, opts...)
	if err != nil {
		return resp, nil, utils.GRPCErrorColored(err)
	}

	data, err := profiling.FromTrailer(trailer)
	if err != nil {
		return resp, nil, err
	}

	return resp, data, nil
}

func (c *Client) Run(ctx context.Context, args *daemon.RunReq, opts ...grpc.CallOption) (*daemon.RunResp, *profiling.Data, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_RUN_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	resp, err := c.daemonClient.Run(ctx, args, opts...)
	if err != nil {
		return resp, nil, utils.GRPCErrorColored(err)
	}

	data, err := profiling.FromTrailer(trailer)
	if err != nil {
		return resp, nil, err
	}

	return resp, data, nil
}

func (c *Client) Manage(ctx context.Context, args *daemon.RunReq, opts ...grpc.CallOption) (*daemon.RunResp, *profiling.Data, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_MANAGE_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	resp, err := c.daemonClient.Manage(ctx, args, opts...)
	if err != nil {
		return resp, nil, utils.GRPCErrorColored(err)
	}

	data, err := profiling.FromTrailer(trailer)
	if err != nil {
		return resp, nil, err
	}

	return resp, data, nil
}

func (c *Client) Get(ctx context.Context, args *daemon.GetReq, opts ...grpc.CallOption) (*daemon.GetResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DB_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)
	resp, err := c.daemonClient.Get(ctx, args, opts...)
	return resp, utils.GRPCErrorColored(err)
}

func (c *Client) List(ctx context.Context, args *daemon.ListReq, opts ...grpc.CallOption) (*daemon.ListResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DB_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)
	resp, err := c.daemonClient.List(ctx, args, opts...)
	return resp, utils.GRPCErrorColored(err)
}

func (c *Client) Kill(ctx context.Context, args *daemon.KillReq, opts ...grpc.CallOption) (*daemon.KillResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DB_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)
	resp, err := c.daemonClient.Kill(ctx, args, opts...)
	return resp, utils.GRPCErrorColored(err)
}

func (c *Client) Delete(ctx context.Context, args *daemon.DeleteReq, opts ...grpc.CallOption) (*daemon.DeleteResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DB_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)
	resp, err := c.daemonClient.Delete(ctx, args, opts...)
	return resp, utils.GRPCErrorColored(err)
}

// Attach attaches to a managed process/container. Exits the program
// with the exit code of the process.
func (c *Client) Attach(ctx context.Context, args *daemon.AttachReq, opts ...grpc.CallOption) error {
	opts = addDefaultOptions(opts)
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
	outDone := cedana_io.CopyNotify(os.Stdout, stdOut)
	errDone := cedana_io.CopyNotify(os.Stderr, stdErr)
	<-outDone // wait to capture all out
	<-errDone // wait to capture all err

	if err := <-errors; err != nil {
		return utils.GRPCErrorColored(err)
	}

	os.Exit(<-exitCode)

	return nil
}

// Just like attach but returns the streams instead of attaching to stdin/stdout/stderr
func (c *Client) AttachIO(ctx context.Context, args *daemon.AttachReq, opts ...grpc.CallOption) (io.Writer, io.Reader, io.Reader, chan int, chan error, error) {
	opts = addDefaultOptions(opts)
	stream, err := c.daemonClient.Attach(ctx, opts...)
	if err != nil {
		return nil, nil, nil, nil, nil, utils.GRPCErrorColored(err)
	}
	// Send the first request
	if err := stream.Send(args); err != nil {
		return nil, nil, nil, nil, nil, utils.GRPCErrorColored(err)
	}

	stdIn, stdOut, stdErr, exitCode, errors := cedana_io.NewStreamIOMaster(stream)

	return stdIn, stdOut, stdErr, exitCode, errors, nil
}

func (c *Client) GetCheckpoint(ctx context.Context, args *daemon.GetCheckpointReq, opts ...grpc.CallOption) (*daemon.GetCheckpointResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DB_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)
	resp, err := c.daemonClient.GetCheckpoint(ctx, args, opts...)
	return resp, utils.GRPCErrorColored(err)
}

func (c *Client) ListCheckpoints(ctx context.Context, args *daemon.ListCheckpointsReq, opts ...grpc.CallOption) (*daemon.ListCheckpointsResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DB_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)
	resp, err := c.daemonClient.ListCheckpoints(ctx, args, opts...)
	return resp, utils.GRPCErrorColored(err)
}

func (c *Client) DeleteCheckpoint(ctx context.Context, args *daemon.DeleteCheckpointReq, opts ...grpc.CallOption) (*daemon.DeleteCheckpointResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DB_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)
	resp, err := c.daemonClient.DeleteCheckpoint(ctx, args, opts...)
	return resp, utils.GRPCErrorColored(err)
}

func (c *Client) Query(ctx context.Context, args *daemon.QueryReq, opts ...grpc.CallOption) (*daemon.QueryResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_DB_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)
	resp, err := c.daemonClient.Query(ctx, args, opts...)
	return resp, utils.GRPCErrorColored(err)
}

func (c *Client) HealthCheck(ctx context.Context, args *daemon.HealthCheckReq, opts ...grpc.CallOption) (*daemon.HealthCheckResp, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_HEALTH_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)
	resp, err := c.daemonClient.HealthCheck(ctx, args, opts...)
	return resp, utils.GRPCErrorColored(err)
}

func (c *Client) ReloadPlugins(ctx context.Context, args *daemon.Empty, opts ...grpc.CallOption) (*daemon.Empty, error) {
	opts = addDefaultOptions(opts)
	resp, err := c.daemonClient.ReloadPlugins(ctx, args, opts...)
	return resp, utils.GRPCErrorColored(err)
}

///////////////////
//    Helpers    //
///////////////////

func (c *Client) HealthCheckConnection(ctx context.Context, opts ...grpc.CallOption) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, DEFAULT_HEALTH_TIMEOUT)
	defer cancel()
	opts = addDefaultOptions(opts)

	healthClient := grpc_health_v1.NewHealthClient(c)
	resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{}, opts...)
	if err != nil {
		return false, err
	}

	if resp.Status == grpc_health_v1.HealthCheckResponse_SERVING {
		return true, nil
	} else {
		return false, nil
	}
}

func addDefaultOptions(opts []grpc.CallOption) []grpc.CallOption {
	if config.Global.Client.WaitForReady {
		opts = append(opts, grpc.WaitForReady(true))
	}
	return opts
}
