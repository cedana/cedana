package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/checkpoint-restore/go-criu/v7"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
)

const DEFAULT_PROTOCOL = "tcp"

type Server struct {
	grpcServer *grpc.Server
	listener   net.Listener

	criu *criu.Criu // for CRIU operations
	// db  db.DB

	wg  sync.WaitGroup  // for waiting for all background tasks to finish
	ctx context.Context // context alive for the duration of the server

	daemon.UnimplementedDaemonServer
}

type ServeOpts struct {
	UseVSOCK bool
	Port     uint32
	Host     string
}

func NewServer(ctx context.Context, opts *ServeOpts) (*Server, error) {
	ctx = log.With().Str("context", "server").Logger().WithContext(ctx)
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

	criu := criu.MakeCriu()

	// Set custom path if specified in config
	if custom_path := viper.GetString("criu.binary_path"); custom_path != "" {
		criu.SetCriuPath(custom_path)
	}
	server := &Server{
		grpcServer: grpc.NewServer(
			grpc.StreamInterceptor(loggingStreamInterceptor()),
			grpc.UnaryInterceptor(loggingUnaryInterceptor(*opts, machineID, macAddr, hostname)),
		),
		criu: criu,
		// db:              db.NewLocalDB(ctx),
		ctx: ctx,
	}

	daemon.RegisterDaemonServer(server.grpcServer, server)

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
func (s *Server) Start() error {
	// Create a child context for the server
	ctx, cancel := context.WithCancelCause(s.ctx)
	log := log.Ctx(ctx)
	defer cancel(nil)

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
	s.wg.Wait()

	s.Stop()
	log.Debug().Msg("stopped server gracefully")

	return err
}

func (s *Server) Stop() error {
	s.grpcServer.GracefulStop()
	return s.listener.Close()
}

func loggingStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := log.With().Str("context", "daemon").Logger().WithContext(ss.Context())
		log := log.Ctx(ctx)
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
		ctx = log.With().Str("context", "daemon").Logger().WithContext(ctx)
		log := log.Ctx(ctx)

		// log the GetContainerInfo method to trace
		if strings.Contains(info.FullMethod, "GetContainerInfo") {
			log.Trace().Str("method", info.FullMethod).Interface("request", req).Msg("gRPC request received")
		} else {
			log.Debug().Str("method", info.FullMethod).Interface("request", req).Msg("gRPC request received")
		}

		resp, err := handler(ctx, req)

		if err != nil {
			log.Error().Str("method", info.FullMethod).Interface("request", req).Interface("response", resp).Err(err).Msg("gRPC request failed")
		} else {
			log.Debug().Str("method", info.FullMethod).Interface("response", resp).Msg("gRPC request succeeded")
		}

		return resp, err
	}
}
