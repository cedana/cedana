package cmd

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/script"
	"github.com/cedana/cedana/pkg/version"
	"github.com/cedana/cedana/plugins/k8s/internal/eventstream"
	k8scripts "github.com/cedana/cedana/plugins/k8s/scripts"
	"github.com/cedana/cedana/scripts"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	propagatorsdk "github.com/cedana/cedana-propagator-sdk/go"
)

const DAEMON_LOG_PATH = "/host/var/log/cedana-daemon.log"

var containerdAddress = "/run/containerd/containerd.sock"

var (
	cedana     *client.Client
	propagator *propagatorsdk.ApiClient
)

func init() {
	HelperCmd.AddCommand(setupCmd)
	HelperCmd.AddCommand(destroyCmd)
	if addr := os.Getenv("CONTAINERD_ADDRESS"); addr != "" {
		containerdAddress = addr
	}

	script.Source(scripts.Utils)
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

		restorePodIdentityEnv, err := prepareHostPodIdentityEnvironment()
		if err != nil {
			return fmt.Errorf("prepare EKS Pod Identity for host daemon: %w", err)
		}
		defer restorePodIdentityEnv()

		err = script.Run(
			log.With().Str("operation", "setup").Logger().Level(zerolog.DebugLevel).WithContext(ctx),
			script.Chroot("/host", scripts.ResetService),
			script.Chroot("/host", scripts.InstallDeps),
			k8scripts.Install,
			script.Chroot("/host", k8scripts.InstallPlugins),
			script.Chroot("/host", k8scripts.ConfigureKubelet),
			script.Chroot("/host", scripts.ConfigureShm),
			script.Chroot("/host", scripts.InstallService),
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup daemon")
			return fmt.Errorf("error setting up host: %w", err)
		}

		cedana, err = client.New(config.Global.Address, config.Global.Protocol)
		if err != nil {
			log.Error().Err(err).Msg("failed to create client")
			return fmt.Errorf("error creating client: %w", err)
		}
		defer cedana.Close()

		propagator = propagatorsdk.NewClient(config.Global.Connection.URL, config.Global.Connection.AuthToken)

		runHelper(ctx)

		return nil
	},
}

// prepareHostPodIdentityEnvironment translates the projected token's
// container path to the same file under the host kubelet volume. The setup
// command is about to chroot and start a host systemd service, where the
// container-only /var/run/secrets path does not exist.
func prepareHostPodIdentityEnvironment() (restore func(), err error) {
	restore = func() {}
	if config.Global.AWS.CredentialsMode != "eksPodIdentity" {
		return restore, nil
	}

	const tokenEnv = "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE"
	tokenPath := os.Getenv(tokenEnv)
	if tokenPath == "" {
		return restore, fmt.Errorf("%s is not set", tokenEnv)
	}
	tokenInfo, err := os.Stat(tokenPath)
	if err != nil {
		return restore, fmt.Errorf("stat projected token: %w", err)
	}

	pattern := "/host/var/lib/kubelet/pods/*/volumes/kubernetes.io~projected/eks-pod-identity-token/eks-pod-identity-token"
	candidates, err := filepath.Glob(pattern)
	if err != nil {
		return restore, fmt.Errorf("find host projected tokens: %w", err)
	}
	for _, candidate := range candidates {
		candidateInfo, statErr := os.Stat(candidate)
		if statErr != nil || !os.SameFile(tokenInfo, candidateInfo) {
			continue
		}

		hostPath := strings.TrimPrefix(candidate, "/host")
		if err := os.Setenv(tokenEnv, hostPath); err != nil {
			return restore, fmt.Errorf("set host token path: %w", err)
		}
		return func() {
			_ = os.Setenv(tokenEnv, tokenPath)
		}, nil
	}

	return restore, fmt.Errorf("could not map projected token %q to a host kubelet volume", tokenPath)
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

		err := script.Run(
			log.With().Str("operation", "destroy").Logger().Level(zerolog.DebugLevel).WithContext(ctx),
			script.Chroot("/host", scripts.ResetService),
			k8scripts.Uninstall,
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to uninstall cedana")
			return fmt.Errorf("error uninstalling: %w", err)
		}

		return nil
	},
}

// Backoff bounds for reconnecting to the event stream. A connection that stayed
// up for EVENT_STREAM_HEALTHY_AFTER is considered healthy, so the backoff resets.
const (
	EVENT_STREAM_MIN_BACKOFF   = 1 * time.Second
	EVENT_STREAM_MAX_BACKOFF   = 2 * time.Minute
	EVENT_STREAM_HEALTHY_AFTER = 5 * time.Minute
)

// runHelper blocks until ctx is done, then stops the daemon on the host.
// Event stream and log tailing failures are retried, never fatal: the helper
// exiting would restart the pod, which re-runs setup and stops the daemon on
// every restart, taking down cedana on the node for something only the
// checkpoint/restore control plane depends on.
func runHelper(ctx context.Context) {
	log.Info().Str("URL", config.Global.Connection.URL).Msgf("starting helper")

	wg := &sync.WaitGroup{}
	wg.Go(func() { superviseEventStream(ctx) })
	wg.Go(func() { tailDaemonLogs(ctx) })

	<-ctx.Done()
	log.Info().Err(ctx.Err()).Msg("stopping daemon")

	// Stop the daemon before waiting on the event stream, which may be stuck
	// dialing rabbitmq (not cancellable) for up to the dial timeout.
	err := script.Run(context.WithoutCancel(ctx), script.Chroot("/host", scripts.ResetService))
	if err != nil {
		log.Error().Err(err).Msg("error stopping daemon")
	}

	wg.Wait()
}

// superviseEventStream keeps the event stream running until ctx is done,
// reconnecting with exponential backoff whenever it fails.
func superviseEventStream(ctx context.Context) {
	backoff := EVENT_STREAM_MIN_BACKOFF
	for {
		started := time.Now()
		err := runEventStream(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) >= EVENT_STREAM_HEALTHY_AFTER {
			backoff = EVENT_STREAM_MIN_BACKOFF
		}
		// Retries are expected while the broker blips; only a stream that has been
		// down long enough to reach the backoff cap is worth alerting on.
		event := log.Warn()
		if backoff == EVENT_STREAM_MAX_BACKOFF {
			event = log.Error()
		}
		event.Err(err).Dur("retry_in", backoff).Msg("event stream stopped, reconnecting")
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, EVENT_STREAM_MAX_BACKOFF)
	}
}

// runEventStream connects to the event stream and serves it until ctx is done
// or a publisher/consumer fails.
func runEventStream(ctx context.Context) error {
	stream, err := eventstream.New(ctx, cedana, propagator, containerdAddress)
	if err != nil {
		return err
	}
	defer func() {
		if err := stream.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close checkpoint event stream")
		}
	}()

	log.Debug().Msg("listening on event stream for checkpoint requests")
	err = stream.StartCheckpointsPublisher(ctx)
	if err != nil {
		return fmt.Errorf("failed to setup checkpoint publisher: %w", err)
	}
	consumerErr := make(chan error, 2)
	go func() {
		consumerErr <- stream.StartCheckpointsConsumer(ctx)
	}()
	if config.Global.Connection.ClusterID != "" {
		go func() {
			consumerErr <- stream.StartFilesConsumer(ctx)
		}()
	} else {
		log.Warn().Msg("cluster id not configured, checkpoint file and delete requests will not be served")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-consumerErr:
		if err == nil {
			err = fmt.Errorf("consumer closed")
		}
		return fmt.Errorf("rabbitmq consumers stopped: %w", err)
	}
}

// tailDaemonLogs prints the daemon's logs until ctx is done, reopening the
// log file if it is missing or unreadable.
func tailDaemonLogs(ctx context.Context) {
	for {
		err := tailFile(ctx, DAEMON_LOG_PATH)
		if ctx.Err() != nil {
			return
		}
		log.Error().Err(err).Msg("failed to read daemon logs, retrying")
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func tailFile(ctx context.Context, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for ctx.Err() == nil {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				select {
				case <-ctx.Done():
				case <-time.After(1 * time.Second):
				}
				continue
			}
			return err
		}
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 {
			// we don't use the log function as the logs should have their own timing data
			fmt.Println(trimmed)
		}
	}
	return nil
}
