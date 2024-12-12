package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"

	"buf.build/gen/go/cedana/cedana/grpc/go/daemon/daemongrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/internal/logger"
	"github.com/cedana/cedana/internal/server/gpu"
	"github.com/cedana/cedana/internal/server/job"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

const DEFAULT_PROTOCOL = "tcp"

type Server struct {
	grpcServer *grpc.Server
	listener   net.Listener

	criu    *criu.Criu
	jobs    job.Manager
	plugins plugins.Manager
	db      db.DB

	wg       *sync.WaitGroup // for waiting for all background tasks to finish
	lifetime context.Context // context alive for the duration of the server

	machine Machine

	daemongrpc.UnimplementedDaemonServer
}

type ServeOpts struct {
	UseVSOCK bool
	Port     uint32
	Host     string
	Metrics  MetricOpts
}

type MetricOpts struct {
	ASR  bool
	OTel bool
}

type Machine struct {
	ID       string
	MACAddr  string
	Hostname string
}

func NewServer(ctx context.Context, opts *ServeOpts) (*Server, error) {
	ctx = log.With().Str("context", "server").Logger().WithContext(ctx)
	var err error

	wg := &sync.WaitGroup{}

	machineID, err := utils.GetMachineID()
	if err != nil {
		return nil, err
	}

	macAddr, err := utils.GetMACAddress()
	if err != nil {
		return nil, err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	var database db.DB
	if config.Get(config.STORAGE_REMOTE) {
		database = db.NewRemoteDB(
			ctx,
			config.Get(config.CEDANA_URL),
			config.Get(config.CEDANA_AUTH_TOKEN),
		)
	} else {
		database, err = db.NewLocalDB(ctx)
	}

	pluginManager := plugins.NewLocalManager()

	var gpuManager gpu.Manager
	gpuPoolSize := config.Get(config.GPU_POOL_SIZE)
	if gpuPoolSize > 0 {
		gpuManager = gpu.NewPoolManager(gpuPoolSize)
	} else {
		gpuManager = gpu.NewSimpleManager(wg, pluginManager)
	}

	jobManager, err := job.NewManagerLazy(ctx, wg, pluginManager, gpuManager, database)
	if err != nil {
		return nil, err
	}

	criu := criu.MakeCriu()

	// Check if CRIU plugin is installed, then use that binary
	var p *plugins.Plugin
	if p = pluginManager.Get("criu"); p.Status != plugins.Installed {
		// Set custom path if specified in config, as a fallback
		if custom_path := config.Get(config.CRIU_BINARY_PATH); custom_path != "" {
			criu.SetCriuPath(custom_path)
		} else {
			return nil, fmt.Errorf(
				"Please install CRIU plugin, or specify %s in config or env var %s",
				config.CRIU_BINARY_PATH.Key,
				config.CRIU_BINARY_PATH.Env,
			)
		}
	} else {
		criu.SetCriuPath(p.Binaries[0])
	}

	server := &Server{
		lifetime: ctx,
		grpcServer: grpc.NewServer(
			grpc.StreamInterceptor(logger.StreamLogger()),
			grpc.UnaryInterceptor(logger.UnaryLogger()),
		),
		criu:    criu,
		plugins: pluginManager,
		jobs:    jobManager,
		wg:      wg,
		machine: Machine{
			ID:       machineID,
			MACAddr:  macAddr,
			Hostname: hostname,
		},
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

	return server, err
}

// Takes in a context that allows for cancellation from the cmdline
func (s *Server) Launch() error {
	ctx, cancel := context.WithCancelCause(s.lifetime)

	go func() {
		err := s.grpcServer.Serve(s.listener)
		if err != nil {
			cancel(err)
		}
	}()

	log.Info().Str("address", s.listener.Addr().String()).Msg("server listening")

	<-ctx.Done()
	err := ctx.Err()

	// Wait for all background go routines to finish
	// WARN: Careful before changing beflow order, as it may cause deadlock
	s.jobs.GetWG().Wait()
	s.wg.Wait()
	s.Stop()

	return err
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
