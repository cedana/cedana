package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/mdlayher/vsock"
	"github.com/swarnimarun/cadvisor/manager"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	taskgrpc "buf.build/gen/go/cedana/task/grpc/go/_gogrpc"
	task "buf.build/gen/go/cedana/task/protocolbuffers/go"
	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/db"
	"github.com/cedana/cedana/pkg/jobservice"
	"github.com/cedana/cedana/pkg/utils"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthcheckgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

const (
	PROTOCOL              = "tcp"
	CEDANA_CONTAINER_NAME = "binary-container"
	SERVER_LOG_MODE       = os.O_APPEND | os.O_CREATE | os.O_WRONLY
	SERVER_LOG_PERMS      = 0o644
)

type service struct {
	CRIU            *Criu
	fs              *afero.Afero // for dependency-injection of filesystems (useful for testing)
	db              db.DB
	serverCtx       context.Context // context alive for the duration of the server
	wg              sync.WaitGroup  // for waiting for all background tasks to finish
	gpuEnabled      bool
	machineID       string
	cadvisorManager manager.Manager

	// fdStore stores a map of fds used for clh kata restores to persist network fds and send them
	// to the appropriate clh vm api
	fdStore sync.Map

	jobService    *jobservice.JobService
	vmSnapshotter VMSnapshot

	taskgrpc.UnimplementedTaskServiceServer
}

type Server struct {
	grpcServer *grpc.Server
	service    *service
	listener   net.Listener
}

type ServeOpts struct {
	GPUEnabled        bool
	VSOCKEnabled      bool
	CedanaURL         string
	Port              uint32
	MetricsEnabled    bool
	JobServiceEnabled bool
	VMSocketPath      string
}

func NewServer(ctx context.Context, opts *ServeOpts, vmSnapshotter VMSnapshot) (*Server, error) {
	var err error

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

	server := &Server{
		grpcServer: grpc.NewServer(
			grpc.StreamInterceptor(loggingStreamInterceptor()),
			grpc.UnaryInterceptor(loggingUnaryInterceptor(*opts, machineID, macAddr, hostname)),
		),
	}

	healthcheck := health.NewServer()
	healthcheckgrpc.RegisterHealthServer(server.grpcServer, healthcheck)

	var js *jobservice.JobService
	if opts.JobServiceEnabled {
		js, err = jobservice.New()
		if err != nil {
			return nil, err
		}
	}

	var database db.DB
	if viper.GetBool("remote") {
		database = db.NewRemoteDB(ctx, viper.GetString("connection.cedana_url")+"/jobs")
	} else {
		database = db.NewLocalDB(ctx)
	}

	service := &service{
		// criu instantiated as empty, because all criu functions run criu swrk (starting the criu rpc server)
		// instead of leaving one running forever.
		CRIU:            &Criu{},
		fs:              &afero.Afero{Fs: afero.NewOsFs()},
		db:              database,
		serverCtx:       ctx,
		gpuEnabled:      opts.GPUEnabled,
		machineID:       machineID,
		cadvisorManager: nil,
		jobService:      js,
		vmSnapshotter:   vmSnapshotter,
	}

	taskgrpc.RegisterTaskServiceServer(server.grpcServer, service)
	reflection.Register(server.grpcServer)

	var listener net.Listener

	if opts.VSOCKEnabled {
		listener, err = vsock.Listen(opts.Port, nil)
	} else {
		// NOTE: `localhost` server inside kubernetes may or may not work
		// based on firewall and network configuration, it would only work
		// on local system, hence for serving use 0.0.0.0
		address := fmt.Sprintf("%s:%d", services.DEFAULT_HOST, opts.Port)
		listener, err = net.Listen(PROTOCOL, address)
	}

	if err != nil {
		return nil, err
	}
	server.listener = listener
	server.service = service

	healthcheck.SetServingStatus("task.TaskService", healthcheckgrpc.HealthCheckResponse_SERVING)
	return server, err
}

func (s *Server) start(ctx context.Context) error {
	if s.service.jobService != nil {
		go func() {
			if err := s.service.jobService.Start(ctx); err != nil {
				// note: at the time of writing this comment, below is unreachable
				// as we never return an error from the Start function
				log.Error().Err(err).Msg("failed to run job service")
			}
		}()
	}
	return s.grpcServer.Serve(s.listener)
}

func (s *Server) stop() error {
	s.grpcServer.GracefulStop()
	return s.listener.Close()
}

// Takes in a context that allows for cancellation from the cmdline
func StartServer(cmdCtx context.Context, opts *ServeOpts) error {
	// Create a child context for the server
	srvCtx, cancel := context.WithCancelCause(cmdCtx)
	defer cancel(nil)

	// For now we only support CloudHypervisor but we should add to the snappshotter more VMs
	// and choose them at the snapshot function level
	vmSnapshotter := CloudHypervisorVM{}
	server, err := NewServer(srvCtx, opts, &vmSnapshotter)
	if err != nil {
		return err
	}

	go func() {
		if opts.GPUEnabled {
			err = DownloadGPUBinaries(cmdCtx)
			if err != nil {
				cancel(err)
				return
			}
		}

		log.Info().Str("host", services.DEFAULT_HOST).Uint32("port", opts.Port).Msg("server listening")

		err := server.start(srvCtx)
		if err != nil {
			cancel(err)
		}
	}()

	<-srvCtx.Done()
	err = srvCtx.Err()

	// Wait for all background go routines to finish
	server.service.wg.Wait()

	server.stop()
	log.Debug().Msg("stopped RPC server gracefully")

	return err
}

func loggingStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		log.Debug().Str("method", info.FullMethod).Msg("gRPC stream started")

		err := handler(srv, ss)

		if err != nil {
			log.Error().Str("method", info.FullMethod).Err(err).Msg("gRPC stream failed")
		} else {
			log.Debug().Str("method", info.FullMethod).Msg("gRPC stream succeeded")
		}

		return err
	}
}

// TODO NR - this needs a deep copy to properly redact
func loggingUnaryInterceptor(serveOpts ServeOpts, machineID string, macAddr string, hostname string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		tp := otel.GetTracerProvider()
		tracer := tp.Tracer("cedana/api")

		ctx, span := tracer.Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		span.SetAttributes(
			attribute.String("grpc.method", info.FullMethod),
			attribute.String("grpc.request", payloadToJSON(req)),
			attribute.String("server.id", machineID),
			attribute.String("server.mac", macAddr),
			attribute.String("server.hostname", hostname),
			attribute.String("server.opts.cedanaurl", serveOpts.CedanaURL),
			attribute.Bool("server.opts.gpuenabled", serveOpts.GPUEnabled),
		)

		// log the GetContainerInfo method to trace
		if strings.Contains(info.FullMethod, "GetContainerInfo") {
			log.Trace().Str("method", info.FullMethod).Interface("request", req).Msg("gRPC request received")
		} else {
			log.Debug().Str("method", info.FullMethod).Interface("request", req).Msg("gRPC request received")
		}

		resp, err := handler(ctx, req)

		span.SetAttributes(
			attribute.String("grpc.response", payloadToJSON(resp)),
		)

		if err != nil {
			log.Error().Str("method", info.FullMethod).Interface("request", req).Interface("response", resp).Err(err).Msg("gRPC request failed")
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		} else {
			log.Debug().Str("method", info.FullMethod).Interface("response", resp).Msg("gRPC request succeeded")
		}

		return resp, err
	}
}

func (s *service) DetailedHealthCheck(ctx context.Context, req *task.DetailedHealthCheckRequest) (*task.DetailedHealthCheckResponse, error) {
	var unhealthyReasons []string
	resp := &task.DetailedHealthCheckResponse{}

	criuVersion, err := s.CRIU.GetCriuVersion()
	if err != nil {
		resp.UnhealthyReasons = append(unhealthyReasons, fmt.Sprintf("CRIU: %v", err))
	}

	check, err := s.CRIU.Check()
	log.Debug().Str("resp", check).Msg("CRIU check")
	if err != nil {
		resp.UnhealthyReasons = append(unhealthyReasons, fmt.Sprintf("CRIU: %v", err))
	}

	resp.HealthCheckStats = &task.HealthCheckStats{}
	resp.HealthCheckStats.CriuVersion = strconv.Itoa(criuVersion)

	if s.gpuEnabled {
		err = s.GPUHealthCheck(ctx, req, resp)
		if err != nil {
			resp.UnhealthyReasons = append(unhealthyReasons, fmt.Sprintf("Error checking gpu health: %v", err))
		}
	}

	return resp, nil
}
func (s *service) GetConfig(ctx context.Context, req *task.GetConfigRequest) (*task.GetConfigResponse, error) {
	resp := &task.GetConfigResponse{}
	config, err := utils.GetConfig()
	if err != nil {
		return nil, err
	}
	var bytes []byte
	bytes, err = json.Marshal(config)
	if err != nil {
		return nil, err
	}
	resp.JSON = string(bytes)
	return resp, nil
}
