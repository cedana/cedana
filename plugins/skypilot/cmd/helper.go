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
	"github.com/cedana/cedana/plugins/skypilot/internal/eventstream"
	skyscripts "github.com/cedana/cedana/plugins/skypilot/scripts"
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
	HelperCmd.AddCommand(destroyCmd)

	script.Source(scripts.Utils)
}

var HelperCmd = &cobra.Command{
	Use:   "skypilot",
	Short: "Helper for setting up and running with SkyPilot",
	Args:  cobra.ExactArgs(1),
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup cedana for SkyPilot and start eventstream consumer",
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		ctx, cancel := context.WithCancel(cmd.Context())
		wg := &sync.WaitGroup{}

		defer func() {
			cancel()
			wg.Wait()
		}()

		if config.Global.Metrics {
			metrics.Init(ctx, wg, "cedana-skypilot-helper", version.Version)
		}

		err = script.Run(
			log.With().Str("operation", "setup").Logger().Level(zerolog.DebugLevel).WithContext(ctx),
			skyscripts.Install,
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to run skypilot setup scripts")
			return fmt.Errorf("error setting up skypilot: %w", err)
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
	Short: "Cleanup cedana SkyPilot helper",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(cmd.Context())
		wg := &sync.WaitGroup{}

		defer func() {
			cancel()
			wg.Wait()
		}()

		if config.Global.Metrics {
			metrics.Init(ctx, wg, "cedana-skypilot-helper", version.Version)
		}

		err := script.Run(
			log.With().Str("operation", "destroy").Logger().Level(zerolog.DebugLevel).WithContext(ctx),
			skyscripts.Uninstall,
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to uninstall skypilot helper")
			return fmt.Errorf("error uninstalling: %w", err)
		}

		return nil
	},
}

func startHelper(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log.Info().Str("URL", config.Global.Connection.URL).Msgf("starting skypilot helper")

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
				fmt.Println(trimmed)
			}
		}
	}()

	<-ctx.Done()
	log.Info().Err(ctx.Err()).Msg("stopping skypilot helper")

	return nil
}
