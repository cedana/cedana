package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cedana/cedana/api/runc"
	"github.com/cedana/cedana/api/services/gpu"
	task "github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/db"
	"github.com/cedana/cedana/utils"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthcheckgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

const (
	ADDRESS                 = "0.0.0.0:8080"
	PROTOCOL                = "tcp"
	CEDANA_CONTAINER_NAME   = "binary-container"
	SERVER_LOG_MODE         = os.O_APPEND | os.O_CREATE | os.O_WRONLY
	SERVER_LOG_PERMS        = 0o644
	GPU_CONTROLLER_LOG_PATH = "/tmp/cedana-gpucontroller.log"
)

type service struct {
	CRIU      *Criu
	fs        *afero.Afero // for dependency-injection of filesystems (useful for testing)
	db        db.DB
	logger    *zerolog.Logger
	store     *utils.CedanaStore
	serverCtx context.Context // context alive for the duration of the server
	wg        sync.WaitGroup  // for waiting for all background tasks to finish

	task.UnimplementedTaskServiceServer
}

type Server struct {
	grpcServer *grpc.Server
	service    *service
	listener   net.Listener
}

func NewServer(ctx context.Context) (*Server, error) {
	logger := ctx.Value("logger").(*zerolog.Logger)

	server := &Server{
		grpcServer: grpc.NewServer(
			grpc.StreamInterceptor(loggingStreamInterceptor(logger)),
			grpc.UnaryInterceptor(loggingUnaryInterceptor(logger)),
			grpc.StatsHandler(otelgrpc.NewServerHandler()),
		),
	}

	healthcheck := health.NewServer()
	healthcheckgrpc.RegisterHealthServer(server.grpcServer, healthcheck)

	service := &service{
		// criu instantiated as empty, because all criu functions run criu swrk (starting the criu rpc server)
		// instead of leaving one running forever.
		CRIU:      &Criu{},
		fs:        &afero.Afero{Fs: afero.NewOsFs()},
		db:        db.NewLocalDB(ctx),
		logger:    logger,
		store:     utils.NewCedanaStore(logger),
		serverCtx: ctx,
	}

	task.RegisterTaskServiceServer(server.grpcServer, service)
	reflection.Register(server.grpcServer)

	listener, err := net.Listen(PROTOCOL, ADDRESS)
	if err != nil {
		return nil, err
	}
	server.listener = listener
	server.service = service

	healthcheck.SetServingStatus("task.TaskService", healthcheckgrpc.HealthCheckResponse_SERVING)
	return server, err
}

func (s *Server) start() error {
	return s.grpcServer.Serve(s.listener)
}

func (s *Server) stop() error {
	s.grpcServer.GracefulStop()
	return s.listener.Close()
}

// Takes in a context that allows for cancellation from the cmdline
func StartServer(cmdCtx context.Context) error {
	logger := cmdCtx.Value("logger").(*zerolog.Logger)

	// Create a child context for the server
	srvCtx, cancel := context.WithCancelCause(cmdCtx)
	defer cancel(nil)

	server, err := NewServer(srvCtx)
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
				cancel(fmt.Errorf("Error opening network namespace: %v", err))
				return
			}
			defer unix.Close(nsFd)

			// Join the network namespace of the target process
			err = unix.Setns(nsFd, unix.CLONE_NEWNET)
			if err != nil {
				cancel(fmt.Errorf("Error setting network namespace: %v", err))
			}
		}

		logger.Debug().Str("Address", ADDRESS).Msgf("server listening")
		err := server.start()
		if err != nil {
			cancel(err)
		}
	}()

	<-srvCtx.Done()
	err = srvCtx.Err()

	// Wait for all background go routines to finish
	server.service.wg.Wait()

	server.stop()
	logger.Debug().Msg("stopped RPC server gracefully")

	return err
}

func (s *service) StartGPUController(ctx context.Context, uid, gid int32, groups []int32, logger *zerolog.Logger) (*exec.Cmd, error) {
	logger.Debug().Int32("UID", uid).Int32("GID", gid).Ints32("Groups", groups).Msgf("starting gpu controller")
	var gpuCmd *exec.Cmd
	controllerPath := viper.GetString("gpu_controller_path")
	if controllerPath == "" {
		controllerPath = utils.GpuControllerBinaryPath
	}
	if _, err := os.Stat(controllerPath); os.IsNotExist(err) {
		logger.Fatal().Err(err)
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
		// wrap controller path in a string
		logger.Info().Str("Args", controllerPath).Msgf("GPU controller started")
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

	gpuCmd.Stderr = nil
	gpuCmd.Stdout = nil

	err := gpuCmd.Start()
	go func() {
		err := gpuCmd.Wait()
		if err != nil {
			logger.Fatal().Err(err)
		}
	}()
	if err != nil {
		logger.Fatal().Err(err)
	}

	// poll gpu controller to ensure it is running
	var opts []grpc.DialOption
	var gpuConn *grpc.ClientConn
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	for {
		gpuConn, err = grpc.Dial("127.0.0.1:50051", opts...)
		if err == nil {
			break
		}
		logger.Info().Msgf("No connection with gpu-controller, waiting 1 sec and trying again...")
		time.Sleep(1 * time.Second)

	}
	defer gpuConn.Close()

	gpuServiceConn := gpu.NewCedanaGPUClient(gpuConn)

	args := gpu.StartupPollRequest{}
	for {
		resp, err := gpuServiceConn.StartupPoll(ctx, &args)
		if err == nil && resp.Success {
			break
		}
		logger.Info().Msgf("Waiting for gpu-controller to start...")
		time.Sleep(1 * time.Second)
	}

	logger.Info().Int("PID", gpuCmd.Process.Pid).Str("Log", GPU_CONTROLLER_LOG_PATH).Msgf("GPU controller started")
	return gpuCmd, nil
}

func loggingStreamInterceptor(logger *zerolog.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		logger.Debug().Str("method", info.FullMethod).Msg("gRPC stream started")

		err := handler(srv, ss)

		if err != nil {
			logger.Error().Str("method", info.FullMethod).Err(err).Msg("gRPC stream failed")
		} else {
			logger.Debug().Str("method", info.FullMethod).Msg("gRPC stream succeeded")
		}

		return err
	}
}

func redactValues(req interface{}, keys, sensitiveSubstrings []string) interface{} {
	val := reflect.Indirect(reflect.ValueOf(req))
	if !val.IsValid() {
		return req
	}

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldName := val.Type().Field(i).Name

		if field.Kind() == reflect.String {
			isSensitive := false
			for _, substring := range sensitiveSubstrings {
				if strings.Contains(fieldName, substring) {
					isSensitive = true
					break
				}
			}

			if isSensitive {
				field.SetString("REDACTED")
				continue
			}

			for _, key := range keys {
				if fieldName == key {
					field.SetString("REDACTED")
					break
				}
			}
		}
	}

	return req
}

func loggingUnaryInterceptor(logger *zerolog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		redactedKeys := []string{"RegistryAuthToken"}
		sensitiveSubstrings := []string{"KEY", "SECRET", "TOKEN", "PASSWORD", "AUTH", "CERT", "API"}

		redactedRequest := redactValues(req, redactedKeys, sensitiveSubstrings)
		logger.Debug().Str("method", info.FullMethod).Interface("request", redactedRequest).Msg("gRPC request received")

		resp, err := handler(ctx, req)

		if err != nil {
			logger.Error().Str("method", info.FullMethod).Interface("request", req).Interface("response", resp).Err(err).Msg("gRPC request failed")
		} else {
			logger.Debug().Str("method", info.FullMethod).Interface("response", resp).Msg("gRPC request succeeded")
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

	resp.HealthCheckStats = &task.HealthCheckStats{}
	resp.HealthCheckStats.CriuVersion = strconv.Itoa(criuVersion)

	// TODO NR - Add CRIU check to output
	err = s.GPUHealthCheck(ctx, req, resp)
	if err != nil {
		resp.UnhealthyReasons = append(unhealthyReasons, fmt.Sprintf("Error checking gpu health: %v", err))
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

	cmd, err := s.StartGPUController(ctx, req.UID, req.GID, req.Groups, s.logger)
	if err != nil {
		resp.UnhealthyReasons = append(resp.UnhealthyReasons, fmt.Sprintf("could not start gpu controller %v", err))
		return nil
	}

	defer cmd.Process.Kill()

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	gpuConn, err := grpc.Dial("127.0.0.1:50051", opts...)
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
		resp.UnhealthyReasons = append(resp.UnhealthyReasons, fmt.Sprintf("gpu health check did not return success"))
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
