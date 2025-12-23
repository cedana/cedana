package cmd

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/version"
	"github.com/cedana/cedana/plugins/k8s/internal/eventstream"
	"github.com/cedana/cedana/plugins/k8s/pkg/utils"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	cedanagosdk "github.com/cedana/cedana-go-sdk"
)

const DAEMON_LOG_PATH = "/host/var/log/cedana-daemon.log"

var containerdAddress = "/run/containerd/containerd.sock"

//go:embed scripts/setup.sh
var setupScript string

//go:embed scripts/start.sh
var startScript string

//go:embed scripts/stop.sh
var stopScript string

//go:embed scripts/cleanup.sh
var cleanupScript string

var (
	cedana     *client.Client
	propagator *cedanagosdk.ApiClient
)

func init() {
	HelperCmd.AddCommand(setupCmd)
	HelperCmd.AddCommand(destroyCmd)
	if addr := os.Getenv("CONTAINERD_ADDRESS"); addr != "" {
		containerdAddress = addr
	}
}

var HelperCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Helper for setting up and running in Kubernetes",
	Args:  cobra.ExactArgs(1),
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup and start cedana on host",
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		ctx, cancel := context.WithCancel(cmd.Context())
		wg := &sync.WaitGroup{}

		defer func() {
			cancel()
			wg.Wait()
		}()

		if config.Global.Metrics {
			metrics.Init(ctx, wg, "cedana-helper", version.Version)
		}

		err = setupDaemon(
			ctx,
			logging.Writer(
				log.With().Str("operation", "setup").Logger().WithContext(ctx),
				zerolog.DebugLevel,
			),
		)
		if err != nil {
			return fmt.Errorf("error setting up host: %w", err)
		}

		cedana, err = client.New(config.Global.Address, config.Global.Protocol)
		if err != nil {
			return fmt.Errorf("error creating client: %w", err)
		}
		defer cedana.Close()

		propagator = cedanagosdk.NewCedanaClient(config.Global.Connection.URL, config.Global.Connection.AuthToken)

		return startHelper(ctx)
	},
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy and cleanup cedana on host",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(cmd.Context())
		wg := &sync.WaitGroup{}

		defer func() {
			cancel()
			wg.Wait()
		}()

		if config.Global.Metrics {
			metrics.Init(ctx, wg, "cedana-helper", version.Version)
		}

		return destroyDaemon(
			ctx,
			logging.Writer(
				log.With().Str("operation", "destroy").Logger().WithContext(ctx),
				zerolog.DebugLevel,
			),
		)
	},
}

func setupDaemon(ctx context.Context, logger ...io.Writer) error {
	return utils.RunScript(ctx, setupScript, logger...)
}

func startDaemon(ctx context.Context) error {
	return utils.RunScript(ctx, startScript)
}

func stopDaemon(ctx context.Context) error {
	return utils.RunScript(context.WithoutCancel(ctx), stopScript)
}

func destroyDaemon(ctx context.Context, logger ...io.Writer) error {
	return utils.RunScript(context.WithoutCancel(ctx), cleanupScript, logger...)
}

func isDaemonRunning(ctx context.Context) (bool, error) {
	return cedana.HealthCheckConnection(ctx, grpc.WaitForReady(true))
}

func startHelper(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log.Info().Str("URL", config.Global.Connection.URL).Msgf("starting helper")

	stream, err := eventstream.New(ctx, cedana, propagator, containerdAddress)
	if err != nil {
		return err
	}

	go func() {
		defer cancel()
		defer stream.Close()
		log.Debug().Msg("listening on event stream for checkpoint requests")
		err := stream.StartCheckpointsPublisher(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup checkpoint publisher")
			return
		}
		err = stream.StartCheckpointsConsumer(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup checkpint request consumer")
			return
		}
	}()

	go func() {
		defer cancel()
		file, err := os.Open(DAEMON_LOG_PATH)
		if err != nil {
			log.Error().Err(err).Msg("failed to open daemon logs")
			return
		}
		defer file.Close()

		reader := bufio.NewReader(file)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					time.Sleep(1 * time.Second)
					continue
				}
				log.Error().Err(err).Msg("Error reading from cedana-daemon.log")
				return
			}
			trimmed := strings.TrimSpace(line)
			if len(trimmed) > 0 {
				// we don't use the log function as the logs should have their own timing data
				fmt.Println(trimmed)
			}
		}
	}()

	<-ctx.Done()
	log.Info().Err(ctx.Err()).Msg("stopping daemon")
	err = stopDaemon(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error stopping daemon")
	}

	return nil
}
