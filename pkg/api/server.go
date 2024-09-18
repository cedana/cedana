package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mdlayher/vsock"
	"github.com/swarnimarun/cadvisor/manager"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cedana/cedana/pkg/api/runc"
	"github.com/cedana/cedana/pkg/api/services/gpu"
	task "github.com/cedana/cedana/pkg/api/services/task"
	"github.com/cedana/cedana/pkg/db"
	"github.com/cedana/cedana/pkg/jobservice"
	"github.com/cedana/cedana/pkg/utils"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthcheckgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	DEFAULT_HOST                = "0.0.0.0"
	PROTOCOL                    = "tcp"
	CEDANA_CONTAINER_NAME       = "binary-container"
	SERVER_LOG_MODE             = os.O_APPEND | os.O_CREATE | os.O_WRONLY
	SERVER_LOG_PERMS            = 0o644
	GPU_CONTROLLER_LOG_PATH     = "/tmp/cedana-gpucontroller.log"
	GPU_CONTROLLER_WAIT_TIMEOUT = 5 * time.Second
)

type service struct {
	CRIU            *Criu
	fs              *afero.Afero // for dependency-injection of filesystems (useful for testing)
	db              db.DB
	store           *utils.CedanaStore
	serverCtx       context.Context // context alive for the duration of the server
	wg              sync.WaitGroup  // for waiting for all background tasks to finish
	gpuEnabled      bool
	machineID       string
	cadvisorManager manager.Manager

	jobService *jobservice.JobService

	task.UnimplementedTaskServiceServer
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
}

func NewServer(ctx context.Context, opts *ServeOpts) (*Server, error) {
	var err error

	machineID, err := utils.GetMachineID()
	if err != nil {
		return nil, err
	}

	server := &Server{
		grpcServer: grpc.NewServer(
			grpc.StreamInterceptor(loggingStreamInterceptor()),
			grpc.UnaryInterceptor(loggingUnaryInterceptor(*opts, machineID)),
		),
	}

	healthcheck := health.NewServer()
	healthcheckgrpc.RegisterHealthServer(server.grpcServer, healthcheck)

	var manager manager.Manager
	if opts.MetricsEnabled {
		manager, err = SetupCadvisor(ctx)
		if err != nil {
			log.Error().Err(err).Send()
			return nil, err
		}
	}

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
		cadvisorManager: manager,
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

	server, err := NewServer(srvCtx, opts)
	if err != nil {
		return err
	}

	go func() {
		// Here join netns
		// TODO find pause bundle path
		if viper.GetBool("is_k8s") {
			_, bundle, err := runc.GetContainerIdByName(CEDANA_CONTAINER_NAME, "", K8S_RUNC_ROOT)
			if err != nil {
				cancel(err)
				return
			}

			pausePid, err := runc.GetPausePid(bundle)
			if err != nil {
				cancel(err)
				return
			}

			nsFd, err := unix.Open(fmt.Sprintf("/proc/%s/ns/net", strconv.Itoa(pausePid)), unix.O_RDONLY, 0)
			if err != nil {
				cancel(fmt.Errorf("error opening network namespace: %v", err))
				return
			}
			defer unix.Close(nsFd)

			// Join the network namespace of the target process
			err = unix.Setns(nsFd, unix.CLONE_NEWNET)
			if err != nil {
				cancel(fmt.Errorf("error setting network namespace: %v", err))
			}
		}

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

func (s *service) StartGPUController(ctx context.Context, uid, gid int32, groups []int32, out io.Writer) (*exec.Cmd, error) {
	log.Debug().Int32("UID", uid).Int32("GID", gid).Ints32("Groups", groups).Msgf("starting gpu controller")
	var gpuCmd *exec.Cmd
	controllerPath := viper.GetString("gpu_controller_path")
	if controllerPath == "" {
		controllerPath = utils.GpuControllerBinaryPath
	}
	if _, err := os.Stat(controllerPath); os.IsNotExist(err) {
		log.Fatal().Err(err)
		return nil, fmt.Errorf("no gpu controller at %s", controllerPath)
	}

	if viper.GetBool("gpu_debugging_enabled") {
		controllerPath = strings.Join([]string{
			"compute-sanitizer",
			"--log-file /tmp/cedana-sanitizer.log",
			"--print-level info",
			"--leak-check=full",
			controllerPath,
		},
			" ")
	}

	gpuCmd = exec.CommandContext(s.serverCtx, controllerPath)
	groupsUint32 := make([]uint32, len(groups))
	for i, v := range groups {
		groupsUint32[i] = uint32(v)
	}
	gpuCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		Credential: &syscall.Credential{
			Uid:    uint32(uid),
			Gid:    uint32(gid),
			Groups: groupsUint32,
		},
	}

	if out != nil {
		gpuCmd.Stderr = out
		gpuCmd.Stdout = out
	}

	err := gpuCmd.Start()
	if err != nil {
		return nil, fmt.Errorf("could not start gpu controller %v", err)
	}
	log.Debug().Int("PID", gpuCmd.Process.Pid).Msgf("GPU controller starting...")

	// poll gpu controller to ensure it is running
	var opts []grpc.DialOption
	var gpuConn *grpc.ClientConn
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	for {
		gpuConn, err = grpc.NewClient("127.0.0.1:50051", opts...)
		if err == nil {
			break
		}
		log.Debug().Msgf("No connection with gpu-controller, waiting 1 sec and trying again...")
		time.Sleep(1 * time.Second)
	}
	defer gpuConn.Close()

	gpuServiceConn := gpu.NewCedanaGPUClient(gpuConn)

	args := gpu.StartupPollRequest{}
	timeout := time.Now().Add(GPU_CONTROLLER_WAIT_TIMEOUT)
	for {
		resp, err := gpuServiceConn.StartupPoll(ctx, &args)
		if err != nil {
			log.Debug().Err(err).Msg("gpu controller not started yet")
		}
		if time.Now().After(timeout) {
			return nil, fmt.Errorf("gpu controller did not start in time")
		}
		if err == nil && resp.Success {
			break
		}
		log.Debug().Msgf("Waiting for gpu-controller to start...")
		time.Sleep(1 * time.Second)
	}

	log.Debug().Int("PID", gpuCmd.Process.Pid).Str("Log", GPU_CONTROLLER_LOG_PATH).Msgf("GPU controller started")
	return gpuCmd, nil
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
func loggingUnaryInterceptor(serveOpts ServeOpts, machineID string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		tp := otel.GetTracerProvider()
		tracer := tp.Tracer("cedana/api")

		ctx, span := tracer.Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		span.SetAttributes(
			attribute.String("grpc.method", info.FullMethod),
			attribute.String("grpc.request", payloadToJSON(req)),
			attribute.String("server.id", machineID),
			attribute.String("server.opts.cedanaurl", serveOpts.CedanaURL),
			attribute.Bool("server.opts.gpuenabled", serveOpts.GPUEnabled),
		)

		// log the GetContainerInfo method to trace
		if strings.Contains(info.FullMethod, "TaskService/GetContainerInfo") {
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
	gpuControllerPath := viper.GetString("gpu_controller_path")
	if gpuControllerPath == "" {
		gpuControllerPath = utils.GpuControllerBinaryPath
	}

	gpuSharedLibPath := viper.GetString("gpu_shared_lib_path")
	if gpuSharedLibPath == "" {
		gpuSharedLibPath = utils.GpuSharedLibPath
	}

	if _, err := os.Stat(gpuControllerPath); os.IsNotExist(err) {
		resp.UnhealthyReasons = append(resp.UnhealthyReasons, fmt.Sprintf("gpu controller binary not found at %s", gpuControllerPath))
	}

	if _, err := os.Stat(gpuSharedLibPath); os.IsNotExist(err) {
		resp.UnhealthyReasons = append(resp.UnhealthyReasons, fmt.Sprintf("gpu shared lib not found at %s", gpuSharedLibPath))
	}

	if len(resp.UnhealthyReasons) != 0 {
		return nil
	}

	gpuOutBuf := &bytes.Buffer{}
	gpuOut := io.MultiWriter(gpuOutBuf)
	cmd, err := s.StartGPUController(ctx, req.UID, req.GID, req.Groups, gpuOut)
	if err != nil {
		log.Error().Err(err).Str("stdout/stderr", gpuOutBuf.String()).Msg("could not start gpu controller")
		resp.UnhealthyReasons = append(resp.UnhealthyReasons, fmt.Sprintf("could not start gpu controller: %v", err))
		return nil
	}

	defer func() {
		err = cmd.Process.Kill()
		if err != nil {
			log.Fatal().Err(err)
		}
		log.Info().Int("PID", cmd.Process.Pid).Msgf("GPU controller killed")
	}()

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	gpuConn, err := grpc.NewClient("127.0.0.1:50051", opts...)
	if err != nil {
		return err
	}

	defer gpuConn.Close()

	gpuServiceConn := gpu.NewCedanaGPUClient(gpuConn)

	args := gpu.HealthCheckRequest{}
	gpuResp, err := gpuServiceConn.HealthCheck(ctx, &args)
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
		log.Debug().Str("Path", filePath).Msgf("GPU binary exists. Delete existing binary to download latest version.")
		// TODO NR - check version and checksum of binary?
		return nil
	}
	log.Debug().Msgf("pulling gpu binary %s", binary)

	url := viper.GetString("connection.cedana_url") + "/checkpoint/gpu/" + binary
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
		log.Error().Err(err).Int("status", resp.StatusCode).Msg("could not get gpu binary")
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
