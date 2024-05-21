package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/cedana/cedana/api/runc"
	task "github.com/cedana/cedana/api/services/task"
	DB "github.com/cedana/cedana/db"
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	ADDRESS               = "localhost:8080"
	PROTOCOL              = "tcp"
	CEDANA_CONTAINER_NAME = "binary-container"
	SERVER_LOG_PATH       = "/var/log/cedana-daemon.log"
	SERVER_LOG_MODE       = os.O_APPEND | os.O_CREATE | os.O_WRONLY
	SERVER_LOG_PERMS      = 0o644
	SERVER_DB_PATH        = "/tmp/cedana.db"
)

type service struct {
	CRIU    *Criu
	fs      *afero.Afero // for dependency-injection of filesystems (useful for testing)
	db      DB.DB        // Key-value store for metadata/state
	logger  *zerolog.Logger
	tracer  trace.Tracer
	store   *utils.CedanaStore
	logFile *os.File // for streaming and storing logs

	task.UnimplementedTaskServiceServer
}

type Server struct {
	grpcServer *grpc.Server
	service    *service
	listener   net.Listener
}

func NewServer(ctx context.Context) (*Server, error) {
	server := &Server{
		grpcServer: grpc.NewServer(),
	}
	logger := ctx.Value("logger").(*zerolog.Logger)
	logFile, err := os.OpenFile(SERVER_LOG_PATH, SERVER_LOG_MODE, SERVER_LOG_PERMS)
	if err != nil {
		logger.Warn().Msgf("failed to open log file %s", SERVER_LOG_PATH)
	}
	// Add log file to logger as a sink
	// This will be read when streaming logs
	newLogger := logger.With().Logger().Output(io.MultiWriter(zerolog.ConsoleWriter{
		Out: os.Stdout,
	}, zerolog.ConsoleWriter{
		Out:        logFile,
		TimeFormat: utils.LOG_TIME_FORMAT_FULL,
	}))

	tracer := otel.GetTracerProvider().Tracer("cedana-daemon")
	service := &service{
		CRIU:    &Criu{},
		fs:      &afero.Afero{Fs: afero.NewOsFs()},
		db:      DB.NewLocalDB(SERVER_DB_PATH),
		logger:  &newLogger,
		tracer:  tracer,
		store:   utils.NewCedanaStore(tracer, logger),
		logFile: logFile,
	}
	task.RegisterTaskServiceServer(server.grpcServer, service)
	reflection.Register(server.grpcServer)

	listener, err := net.Listen(PROTOCOL, ADDRESS)
	if err != nil {
		return nil, err
	}
	server.listener = listener
	server.service = service

	return server, err
}

func (s *Server) start() error {
	return s.grpcServer.Serve(s.listener)
}

func (s *Server) stop() error {
	s.grpcServer.GracefulStop()
	s.service.logFile.Close()
	return s.listener.Close()
}

// Takes in a context that allows for cancellation from the cmdline
func StartServer(cmdCtx context.Context) error {
	logger := cmdCtx.Value("logger").(*zerolog.Logger)

	// Create a child context for the server
	srvCtx, cancel := context.WithCancelCause(cmdCtx)
	defer cancel(nil)

	server, err := NewServer(cmdCtx)
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

		logger.Debug().Msgf("server listening at %s", ADDRESS)
		err := server.start()
		if err != nil {
			cancel(err)
		}
	}()

	select {
	case <-srvCtx.Done():
		err = srvCtx.Err()
		logger.Debug().Msg("stopped RPC server unexpectedly")
	case <-cmdCtx.Done():
		err = cmdCtx.Err()
		server.stop()
		logger.Debug().Msg("stopped RPC server gracefully")
	}

	return err
}

func StartGPUController(uid, gid uint32, logger *zerolog.Logger) (*exec.Cmd, error) {
	logger.Debug().Msgf("starting gpu controller with uid: %d, gid: %d", uid, gid)
	var gpuCmd *exec.Cmd
	controllerPath := viper.GetString("gpu_controller_path")
	if controllerPath == "" {
		err := fmt.Errorf("gpu controller path not set")
		logger.Fatal().Err(err)
		return nil, err
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
		logger.Info().Msgf("GPU controller started with args: %v", controllerPath)
	}

	gpuCmd = exec.Command("bash", "-c", controllerPath)
	gpuCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		Credential: &syscall.Credential{
			Uid: uid,
			Gid: gid,
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
	logger.Info().Msgf("GPU controller started with pid: %d, logging to: /tmp/cedana-gpucontroller.log", gpuCmd.Process.Pid)
	return gpuCmd, nil
}
