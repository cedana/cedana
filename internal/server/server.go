package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DEFAULT_HOST          = "0.0.0.0"
  DEFAULT_PORT          = 8080
	DEFAULT_PROTOCOL      = "tcp"
)

type Server struct {
	grpcServer *grpc.Server
	listener   net.Listener

	criu       *Criu
	fs         *afero.Afero // for dependency-injection of filesystems (useful for testing)
	db         db.DB
	ctx        context.Context // context alive for the duration of the server
	wg         sync.WaitGroup  // for waiting for all background tasks to finish

	task.UnimplementedTaskServiceServer
}

type ServeOpts struct {
	VSOCKEnabled      bool
	Port              uint32
  Host              string
}

func NewServer(ctx context.Context, opts *ServeOpts) (*Server, error) {
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

	service := &service{
		// criu instantiated as empty, because all criu functions run criu swrk (starting the criu rpc server)
		// instead of leaving one running forever.
		CRIU:            &Criu{},
		fs:              &afero.Afero{Fs: afero.NewOsFs()},
		db:              db.NewLocalDB(ctx),
		store:           utils.NewCedanaStore(),
		serverCtx:       ctx,
		gpuEnabled:      opts.GPUEnabled,
		machineID:       machineID,
		cadvisorManager: nil,
		jobService:      js,
	}

	task.RegisterTaskServiceServer(server.grpcServer, service)
	reflection.Register(server.grpcServer)

	var listener net.Listener

	if opts.VSOCKEnabled {
		listener, err = vsock.Listen(opts.Port, nil)
	} else {
		// NOTE: `localhost` server inside kubernetes may or may not work
		// based on firewall and network configuration, it would only work
		// on local system, hence for serving use 0.0.0.0
		address := fmt.Sprintf("%s:%d", DEFAULT_HOST, opts.Port)
		listener, err = net.Listen(DEFAULT_PROTOCOL, address)
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

	server, err := NewServer(srvCtx, opts)
	if err != nil {
		return err
	}

	go func() {
		if opts.GPUEnabled {
			if viper.GetString("gpu_controller_path") == "" {
				err = pullGPUBinary(cmdCtx, utils.GpuControllerBinaryName, utils.GpuControllerBinaryPath)
				if err != nil {
					log.Error().Err(err).Msg("could not pull gpu controller")
					cancel(err)
					return
				}
			} else {
				log.Debug().Str("path", viper.GetString("gpu_controller_path")).Msg("using gpu controller")
			}

			if viper.GetString("gpu_shared_lib_path") == "" {
				err = pullGPUBinary(cmdCtx, utils.GpuSharedLibName, utils.GpuSharedLibPath)
				if err != nil {
					log.Error().Err(err).Msg("could not pull gpu shared lib")
					cancel(err)
					return
				}
			} else {
				log.Debug().Str("path", viper.GetString("gpu_shared_lib_path")).Msg("using gpu shared lib")
			}
		}

		log.Info().Str("host", DEFAULT_HOST).Uint32("port", opts.Port).Msg("server listening")

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

func (s *service) GPUHealthCheck(
	ctx context.Context,
	req *task.DetailedHealthCheckRequest,
	resp *task.DetailedHealthCheckResponse,
) error {
	// create dummy job for doing health check
	job := Job{Id: fmt.Sprintf("healthcheck-%d", time.Now().Unix()), LifetimeCtx: s.serverCtx}
	err := job.startGPUController(ctx, req.UID, req.GID, req.Groups)
	if err != nil {
		log.Error().Err(err).Str("stdout/stderr", job.gpuOutBuf.String()).Msg("could not start gpu controller")
		resp.UnhealthyReasons = append(resp.UnhealthyReasons, fmt.Sprintf("could not start gpu controller: %v", err))
		return nil
	}
	defer job.stopGPUController()

	gpuResp, err := job.gpuClient.HealthCheck(ctx, &gpu.HealthCheckRequest{})
	if err != nil {
		return err
	}

	if !gpuResp.Success {
		resp.UnhealthyReasons = append(resp.UnhealthyReasons, "gpu health check did not return success")
	}

	resp.HealthCheckStats.GPUHealthCheck = gpuResp

	return nil
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

func pullGPUBinary(ctx context.Context, binary string, filePath string) error {
	_, err := os.Stat(filePath)
	if err == nil {
		log.Info().Str("Path", filePath).Msgf("GPU binary exists. Delete existing binary to download latest version.")
		// TODO NR - check version and checksum of binary?
		return nil
	}
	log.Debug().Msgf("pulling gpu binary %s", binary)

	url := viper.GetString("connection.cedana_url") + "/k8s/gpu/" + binary
	log.Debug().Msgf("pulling %s from %s", binary, url)

	httpClient := &http.Client{}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Err(err).Msg("failed to build http post request with jsonBody")
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			log.Error().Err(err).Int("status", resp.StatusCode).Msg("could not get gpu binary")
		} else {
			log.Error().Err(err).Msg("could not get gpu binary")
		}
		return fmt.Errorf("could not get gpu binary")
	}
	defer resp.Body.Close()

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0755)
	if err == nil {
		err = os.Chmod(filePath, 0755)
	}
	if err != nil {
		log.Err(err).Msg("could not create file")
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		log.Err(err).Msg("could not read file from response")
		return err
	}
	log.Debug().Msgf("%s downloaded to %s", binary, filePath)
	return err
}

func payloadToJSON(payload any) string {
	if payload == nil {
		return "null"
	}

	protoMsg, ok := payload.(proto.Message)
	if !ok {
		return fmt.Sprintf("%+v", payload)
	}

	marshaler := protojson.MarshalOptions{
		EmitUnpopulated: true,
		Indent:          "  ",
	}
	jsonData, err := marshaler.Marshal(protoMsg)
	if err != nil {
		return fmt.Sprintf("Error marshaling to JSON: %v", err)
	}

	return string(jsonData)
}
