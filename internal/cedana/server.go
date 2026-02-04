package cedana

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"buf.build/gen/go/cedana/cedana/grpc/go/daemon/daemongrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana/gpu"
	"github.com/cedana/cedana/internal/cedana/job"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/cedana/cedana/pkg/version"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Server is the main server struct that holds all the components of the Cedana server.
type Server struct {
	Cedana

	grpcServer   *grpc.Server
	healthServer *health.Server
	listener     net.Listener

	// fdStore stores a map of fds used for clh kata restores to persist network fds and send them
	// to the appropriate clh vm api
	fdStore sync.Map
	jobs    job.Manager
	db      db.DB

	host    *daemon.Host
	version string

	daemongrpc.UnimplementedDaemonServer

	ipEventCh   chan *daemon.IPReportReq
	pendingMaps sync.Map
}

type ServeOpts struct {
	Address  string
	Protocol string
	Version  string
}

func NewServer(ctx context.Context, opts *ServeOpts) (server *Server, err error) {
	wg := &sync.WaitGroup{}

	if config.Global.Metrics {
		metrics.Init(ctx, wg, "cedana", version.Version)
	}

	host, err := utils.GetHost(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host info: %w", err)
	}

	var database db.DB
	database, err = db.NewSqliteDB(ctx, config.Global.DB.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to create local sqlite db: %w", err)
	}

	if config.Global.DB.Remote {
		database = db.NewPropagatorDB(ctx, config.Global.Connection, database)
	}

	err = database.PutHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to put host info: %w", err)
	}

	pluginManager := plugins.NewLocalManager()

	gpuPoolSize := config.Global.GPU.PoolSize
	gpuManager, err := gpu.NewPoolManager(ctx, wg, gpuPoolSize, pluginManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create GPU manager: %w", err)
	}

	jobManager, err := job.NewManagerLazy(ctx, wg, host, pluginManager, gpuManager, database)
	if err != nil {
		return nil, fmt.Errorf("failed to create job manager: %w", err)
	}

	server = &Server{
		Cedana: Cedana{
			gpus:     gpuManager,
			plugins:  pluginManager,
			wg:       wg,
			lifetime: ctx,
		},
		grpcServer: grpc.NewServer(
			grpc.ChainStreamInterceptor(
				logging.StreamLogger(),
			),
			grpc.ChainUnaryInterceptor(
				logging.UnaryLogger(),
				profiling.UnaryProfiler(),
			),
		),
		healthServer: health.NewServer(),
		db:           database,
		jobs:         jobManager,
		host:         host,
		version:      opts.Version,
		ipEventCh:    make(chan *daemon.IPReportReq, config.DEFAULT_MULTINODE_BUFFER),
	}

	daemongrpc.RegisterDaemonServer(server.grpcServer, server)
	grpc_health_v1.RegisterHealthServer(server.grpcServer, server.healthServer)
	reflection.Register(server.grpcServer)

	var listener net.Listener

	protocol := strings.ToLower(opts.Protocol)
	address := opts.Address

	switch protocol {
	case "tcp":
		if address == "" {
			address = config.DEFAULT_TCP_ADDR
		}
		listener, err = net.Listen("tcp", address)
	case "unix":
		if address == "" {
			address = config.DEFAULT_SOCK_ADDR
		}
		listener, err = net.Listen("unix", address)
		if err == nil {
			err = os.Chmod(address, config.DEFAULT_SOCK_PERMS)
		}
	case "vsock":
		if address == "" {
			return nil, fmt.Errorf("vsock address is required")
		}
		if !strings.Contains(address, ":") {
			return nil, fmt.Errorf("vsock address must be in the format cid:port")
		}
		portStr := strings.Split(address, ":")[1]
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse vsock port: %w", err)
		}
		listener, err = vsock.Listen(uint32(port), nil)
	default:
		err = fmt.Errorf("invalid protocol: %s", protocol)
	}
	if err != nil {
		return nil, err
	}

	server.listener = listener

	log.Info().Str("mac", host.MAC).Str("id", host.ID).Str("hostname", host.Hostname).Msg("host")

	return server, err
}

// Takes in a context that allows for cancellation from the cmdline
func (s *Server) Launch(ctx context.Context) (err error) {
	lifetime, cancel := context.WithCancel(ctx)
	s.lifetime = lifetime
	s.cancel = cancel

	go func() {
		err := s.grpcServer.Serve(s.listener)
		if err != nil {
			cancel()
		}
		s.healthServer.Shutdown()
	}()

	s.healthServer.Resume()
	log.Info().Str("address", s.listener.Addr().String()).Msg("server listening")

	<-lifetime.Done()
	err = lifetime.Err()

	s.Stop()

	return
}

func (s *Server) Stop() {
	s.grpcServer.GracefulStop()
	s.listener.Close()
	s.Wait()
	log.Info().Msg("stopped server gracefully")
}

func (s *Server) ReloadPlugins(ctx context.Context, req *daemon.Empty) (*daemon.Empty, error) {
	plugins.Load()

	return &daemon.Empty{}, nil
}
