package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/script"
	"github.com/cedana/cedana/pkg/version"
	"github.com/cedana/cedana/plugins/bridge/internal/eventstream"
	bridgescripts "github.com/cedana/cedana/plugins/bridge/scripts"
	"github.com/cedana/cedana/scripts"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	cedanagosdk "github.com/cedana/cedana-go-sdk"
)

const DAEMON_LOG_PATH = "/var/log/cedana-daemon.log"

var (
	cedana     *client.Client
	propagator *cedanagosdk.ApiClient
)

func init() {
	HelperCmd.AddCommand(setupCmd)
	HelperCmd.AddCommand(startCmd)
	HelperCmd.AddCommand(destroyCmd)

	script.Source(scripts.Utils)
}

var HelperCmd = &cobra.Command{
	Use:   "bridge",
	Short: "Helper for setting up and running with Bridge",
	Args:  cobra.ExactArgs(1),
}

// setup installs deps, configures the host, and creates+starts the daemon service. Then exits.
var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup cedana daemon service for Bridge (runs once, then exits)",
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		ctx, cancel := context.WithCancel(cmd.Context())
		wg := &sync.WaitGroup{}

		defer func() {
			cancel()
			wg.Wait()
		}()

		if config.Global.Metrics {
			metrics.Init(ctx, wg, "cedana-bridge", version.Version)
		}

		err = script.Run(
			log.With().Str("operation", "setup").Logger().Level(zerolog.DebugLevel).WithContext(ctx),
			scripts.ResetService,
			scripts.InstallDeps,
			bridgescripts.Install,
			scripts.ConfigureShm,
			scripts.InstallService,
			bridgescripts.InstallHelperService,
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup daemon")
			return fmt.Errorf("error setting up host: %w", err)
		}

		log.Info().Msg("daemon and helper services installed and started")
		return nil
	},
}

// start runs the long-lived eventstream consumer (meant to be run via systemd).
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Bridge eventstream consumer (long-running)",
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		ctx, cancel := context.WithCancel(cmd.Context())
		wg := &sync.WaitGroup{}

		defer func() {
			cancel()
			wg.Wait()
		}()

		if config.Global.Metrics {
			metrics.Init(ctx, wg, "cedana-bridge", version.Version)
		}

		cedana, err = client.New(config.Global.Address, config.Global.Protocol)
		if err != nil {
			log.Error().Err(err).Msg("failed to create client")
			return fmt.Errorf("error creating client: %w", err)
		}
		defer cedana.Close()

		propagator = cedanagosdk.NewCedanaClient(config.Global.Connection.URL, config.Global.Connection.AuthToken)

		err = startHelper(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to start helper")
			return fmt.Errorf("error starting helper: %w", err)
		}

		return nil
	},
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Cleanup cedana Bridge helper",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(cmd.Context())
		wg := &sync.WaitGroup{}

		defer func() {
			cancel()
			wg.Wait()
		}()

		if config.Global.Metrics {
			metrics.Init(ctx, wg, "cedana-bridge", version.Version)
		}

		err := script.Run(
			log.With().Str("operation", "destroy").Logger().Level(zerolog.DebugLevel).WithContext(ctx),
			bridgescripts.Uninstall,
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to uninstall bridge helper")
			return fmt.Errorf("error uninstalling: %w", err)
		}

		return nil
	},
}

func startHelper(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log.Info().Str("URL", config.Global.Connection.URL).Msgf("starting bridge helper")

	stream, err := eventstream.New(ctx, cedana, propagator)
	if err != nil {
		return err
	}

	go func() {
		defer cancel()
		defer func() {
			if err := stream.Close(); err != nil {
				log.Error().Err(err).Msg("failed to close checkpoint event stream")
			}
		}()
		log.Debug().Msg("listening on event stream for checkpoint requests")
		err := stream.StartCheckpointsPublisher(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup checkpoint publisher")
			return
		}
		err = stream.StartCheckpointsConsumer(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup checkpoint request consumer")
			return
		}
		err = stream.StartRestoresConsumer(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup restore request consumer")
			return
		}
	}()

	go func() {
		defer cancel()
		// Wait for daemon log file to appear
		var file *os.File
		for i := 0; i < 30; i++ {
			file, err = os.Open(DAEMON_LOG_PATH)
			if err == nil {
				break
			}
			time.Sleep(1 * time.Second)
		}
		if err != nil {
			log.Error().Err(err).Msg("failed to open daemon logs after waiting")
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
				fmt.Println(trimmed)
			}
		}
	}()

	<-ctx.Done()
	log.Info().Err(ctx.Err()).Msg("stopping bridge helper")

	return nil
}
