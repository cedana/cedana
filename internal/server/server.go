package server

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
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/internal/metrics"
	"github.com/cedana/cedana/internal/server/gpu"
	"github.com/cedana/cedana/internal/server/job"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Server struct {
	grpcServer *grpc.Server
	listener   net.Listener

	jobs job.Manager
	gpus gpu.Manager

	db db.DB
	// fdStore stores a map of fds used for clh kata restores to persist network fds and send them
	// to the appropriate clh vm api
	fdStore sync.Map

	lifetime context.Context // context alive for the duration of the server
	wg       *sync.WaitGroup // for waiting for all background tasks to finish
	plugins  plugins.Manager

	host    *daemon.Host
	version string

	daemongrpc.UnimplementedDaemonServer
}

// Root avoids the use of the server and provides direct run/restores
type Root struct {
	lifetime context.Context // context alive for the duration of the server
	wg       *sync.WaitGroup // for waiting for all background tasks to finish
	plugins  plugins.Manager

	host    *daemon.Host
	version string
}

type ServeOpts struct {
	Address  string
	Protocol string
	Version  string
	Metrics  config.Metrics
}

type MetricOpts struct {
	ASR  bool
	OTel bool
}

func (s *Root) Wait() {
	s.wg.Wait()
}

func NewRoot(ctx context.Context) (*Root, error) {
	host, err := utils.GetHost(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host info: %w", err)
	}
	pluginManager := plugins.NewLocalManager()
	var wg = sync.WaitGroup{}
	return &Root{
		lifetime: ctx,
		wg:       &wg,
		plugins:  pluginManager,
		host:     host,
		version:  "dev",
	}, nil
}

func NewServer(ctx context.Context, opts *ServeOpts) (*Server, error) {
	ctx = log.With().Str("context", "server").Logger().WithContext(ctx)
	var err error

	wg := &sync.WaitGroup{}

	host, err := utils.GetHost(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host info: %w", err)
	}

	var database db.DB
	if config.Global.DB.Remote {
		database = db.NewPropagatorDB(ctx, config.Global.Connection)
	} else {
		database, err = db.NewSqliteDB(ctx, config.Global.DB.Path)
	}
	if err != nil {
		return nil, err
	}

	err = database.PutHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to put host info: %w", err)
	}

	pluginManager := plugins.NewLocalManager()

	var gpuManager gpu.Manager
	gpuPoolSize := config.Global.GPU.PoolSize
	if gpuPoolSize > 0 {
		log.Info().Int("pool_size", gpuPoolSize).Msg("GPU pool size set")
		gpuManager = gpu.NewPoolManager(ctx, wg, gpuPoolSize)
	} else {
		gpuManager = gpu.NewSimpleManager(wg, pluginManager)
	}

	jobManager, err := job.NewManagerLazy(ctx, wg, pluginManager, gpuManager, database)
	if err != nil {
		return nil, fmt.Errorf("failed to create job manager: %w", err)
	}

	server := &Server{
		grpcServer: grpc.NewServer(
			grpc.ChainStreamInterceptor(
				logging.StreamLogger(),
				metrics.StreamTracer(host),
			),
			grpc.ChainUnaryInterceptor(
				logging.UnaryLogger(),
				metrics.UnaryTracer(host),
				profiling.UnaryProfiler(),
			),
		),
		jobs:    jobManager,
		gpus:    gpuManager,
		db:      database,
		plugins: pluginManager,
		wg:      wg,
		host:    host,
		version: opts.Version,
	}

	daemongrpc.RegisterDaemonServer(server.grpcServer, server)
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
	lifetime, cancel := context.WithCancelCause(ctx)
	s.lifetime = lifetime

	if config.Global.Metrics.Otel {
		shutdown, _ := metrics.InitOtel(ctx, s.version)
		defer func() {
			err = shutdown(ctx)
		}()
	}

	// Initialize ASR metrics reporting if enabled
	if config.Global.Metrics.ASR {
		cedanaURL := config.Global.Connection.URL
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := metrics.PollAndPublishMetrics(lifetime, cedanaURL); err != nil {
				log.Error().Err(err).Msg("failed to initialize ASR metrics reporting")
				cancel(fmt.Errorf("ASR metrics reporting failed: %w", err))
			}
		}()
	}

	go func() {
		err := s.grpcServer.Serve(s.listener)
		if err != nil {
			cancel(err)
		}
	}()

	log.Info().Str("address", s.listener.Addr().String()).Msg("server listening")

	<-lifetime.Done()
	err = lifetime.Err()

	// Wait for all background go routines to finish
	// WARN: Careful before changing beflow order, as it may cause deadlock
	s.jobs.GetWG().Wait()
	s.wg.Wait()
	s.Stop()

	return
}

func (s *Server) Stop() {
	s.grpcServer.GracefulStop()
	s.listener.Close()
	log.Info().Msg("stopped server gracefully")
}

func (s *Server) ReloadPlugins(ctx context.Context, req *daemon.Empty) (*daemon.Empty, error) {
	plugins.Load()

	return &daemon.Empty{}, nil
}
