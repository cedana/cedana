package server

import (
	"context"
	"fmt"
	"net"
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
)

const DEFAULT_PROTOCOL = "tcp"

type Server struct {
	grpcServer *grpc.Server
	listener   net.Listener

	jobs    job.Manager
	gpus    gpu.Manager
	plugins plugins.Manager
	db      db.DB

	wg       *sync.WaitGroup // for waiting for all background tasks to finish
	lifetime context.Context // context alive for the duration of the server

	host    *daemon.Host
	version string

	daemongrpc.UnimplementedDaemonServer
}

type ServeOpts struct {
	UseVSOCK bool
	Port     uint32
	Host     string
	Version  string
	Metrics  config.Metrics
}

type MetricOpts struct {
	ASR  bool
	OTel bool
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
		database = db.NewRemoteDB(ctx, config.Global.Connection)
	} else {
		database, err = db.NewLocalDB(ctx, config.Global.DB.Path)
	}
	if err != nil {
		return nil, err
	}

	pluginManager := plugins.NewLocalManager()

	var gpuManager gpu.Manager
	gpuPoolSize := config.Global.GPU.PoolSize
	if gpuPoolSize > 0 {
		log.Info().Int("pool_size", gpuPoolSize).Msg("GPU pool size set")
		gpuManager = gpu.NewPoolManager(ctx, wg, gpuPoolSize)
	} else {
		gpuManager = gpu.NewSimpleManager(ctx, wg, pluginManager)
	}

	jobManager, err := job.NewManagerLazy(ctx, wg, pluginManager, gpuManager, database)
	if err != nil {
		return nil, err
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
		plugins: pluginManager,
		jobs:    jobManager,
		gpus:    gpuManager,
		db:      database,
		wg:      wg,
		host:    host,
		version: opts.Version,
	}

	daemongrpc.RegisterDaemonServer(server.grpcServer, server)

	var listener net.Listener

	if opts.UseVSOCK {
		listener, err = vsock.Listen(opts.Port, nil)
	} else {
		// NOTE: `localhost` server inside kubernetes may or may not work
		// based on firewall and network configuration, it would only work
		// on local system, hence for serving use 0.0.0.0
		address := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
		listener, err = net.Listen(DEFAULT_PROTOCOL, address)
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

	shutdown, _ := metrics.InitOtel(ctx, s.version)
	defer func() {
		err = shutdown(ctx)
	}()

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
