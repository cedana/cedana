package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/script"
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/cedana/cedana/pkg/version"
	"github.com/cedana/cedana/plugins/bridge/internal/eventstream"
	bridgescripts "github.com/cedana/cedana/plugins/bridge/scripts"
	"github.com/cedana/cedana/scripts"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
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
	HelperCmd.AddCommand(runCmd)
	HelperCmd.AddCommand(jobCmd)
	HelperCmd.AddCommand(nodeCmd)

	runCmd.AddCommand(runProcessCmd)
	jobCmd.AddCommand(jobListCmd)
	nodeCmd.AddCommand(nodeListCmd)

	runProcessCmd.Flags().String("jid", "", "job id")
	runProcessCmd.Flags().String("out", "", "file to forward stdout/err")
	runProcessCmd.Flags().Bool("attach", false, "attach stdin/out/err")

	jobListCmd.Flags().String("node-id", "", "filter by node id")
	jobListCmd.Flags().String("hostname", "", "filter by hostname")
	jobListCmd.Flags().String("status", "", "filter by status")
	jobListCmd.Flags().Int64("limit", 100, "maximum number of results")
	jobListCmd.Flags().Int64("offset", 0, "number of results to skip")
	jobListCmd.Flags().Bool("json", false, "output raw json")

	nodeListCmd.Flags().String("status", "", "filter by status")
	nodeListCmd.Flags().Int64("limit", 100, "maximum number of results")
	nodeListCmd.Flags().Int64("offset", 0, "number of results to skip")
	nodeListCmd.Flags().Bool("json", false, "output raw json")

	HelperCmd.AddCommand(utils.AliasOf(jobListCmd, "jobs"))
	HelperCmd.AddCommand(utils.AliasOf(nodeListCmd, "nodes"))

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
	var localWG sync.WaitGroup
	errCh := make(chan error, 2)

	log.Info().Str("URL", config.Global.Connection.URL).Msgf("starting bridge helper")

	stream, err := eventstream.New(ctx, cedana, propagator)
	if err != nil {
		return err
	}

	log.Debug().Msg("listening on event stream for checkpoint requests")
	err = stream.StartCheckpointsPublisher(ctx)
	if err != nil {
		return err
	}

	err = stream.StartRestoresPublisher(ctx)
	if err != nil {
		return err
	}

	localWG.Add(2)
	go func() {
		defer localWG.Done()
		log.Debug().Msg("checkpoint consumer starting")
		err := stream.StartCheckpointsConsumer(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup checkpoint request consumer")
			errCh <- fmt.Errorf("checkpoint consumer failed: %w", err)
			cancel()
			return
		}
		if ctx.Err() == nil {
			errCh <- fmt.Errorf("checkpoint consumer stopped unexpectedly")
			cancel()
		}
		log.Debug().Msg("checkpoint consumer stopped")
	}()
	go func() {
		defer localWG.Done()
		log.Debug().Msg("restore consumer starting")
		err := stream.StartRestoresConsumer(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup restore request consumer")
			errCh <- fmt.Errorf("restore consumer failed: %w", err)
			cancel()
			return
		}
		if ctx.Err() == nil {
			errCh <- fmt.Errorf("restore consumer stopped unexpectedly")
			cancel()
		}
		log.Debug().Msg("restore consumer stopped")
	}()

	go func() {
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
			log.Warn().Err(err).Msg("failed to open daemon logs after waiting; log tailing disabled")
			return
		}
		defer file.Close()

		reader := bufio.NewReader(file)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					time.Sleep(1 * time.Second)
					continue
				}
				log.Warn().Err(err).Msg("error reading from cedana-daemon.log; continuing log tail")
				time.Sleep(1 * time.Second)
				continue
			}
			trimmed := strings.TrimSpace(line)
			if len(trimmed) > 0 {
				fmt.Println(trimmed)
			}
		}
	}()

	var runErr error
	select {
	case runErr = <-errCh:
	case <-ctx.Done():
		runErr = nil
	}
	log.Info().Err(ctx.Err()).Msg("stopping bridge helper")
	if err := stream.Close(); err != nil {
		log.Error().Err(err).Msg("failed to close checkpoint event stream")
	}
	localWG.Wait()

	return runErr
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run Bridge-managed workloads",
}

var runProcessCmd = &cobra.Command{
	Use:   "process <path> [args...]",
	Short: "Run a managed process under Bridge",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ced, err := client.New(config.Global.Address, config.Global.Protocol)
		if err != nil {
			return fmt.Errorf("error creating client: %w", err)
		}
		defer ced.Close()

		jid, _ := cmd.Flags().GetString("jid")
		if strings.TrimSpace(jid) == "" {
			return fmt.Errorf("jid is required for bridge run process")
		}
		out, _ := cmd.Flags().GetString("out")
		attach, _ := cmd.Flags().GetBool("attach")

		path := args[0]
		fullPath, err := exec.LookPath(path)
		if err != nil {
			return err
		}
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("error getting working directory: %w", err)
		}
		user, err := utils.GetCredentials()
		if err != nil {
			return fmt.Errorf("error getting credentials: %w", err)
		}

		req := &daemon.RunReq{
			JID:        jid,
			Log:        out,
			GPUEnabled: false,
			GPUTracing: false,
			Attachable: attach,
			Action:     daemon.RunAction_START_NEW,
			Env:        os.Environ(),
			UID:        user.Uid,
			GID:        user.Gid,
			Groups:     user.Groups,
			Type:       "process",
			Details: &daemon.Details{Process: &daemon.Process{
				Path:       fullPath,
				Args:       args[1:],
				WorkingDir: wd,
			}},
		}

		resp, _, err := ced.Run(cmd.Context(), req)
		if err != nil {
			return err
		}
		for _, msg := range resp.GetMessages() {
			fmt.Println(msg)
		}

		hostname, err := os.Hostname()
		if err != nil || strings.TrimSpace(hostname) == "" {
			hostname = "unknown"
		}
		registerReq := map[string]any{
			"job_id":   jid,
			"job_name": path,
			"hostname": hostname,
		}
		if err := postPropagatorJSON(cmd.Context(), "/v2/bridge/jobs/register", registerReq, nil); err != nil {
			_, killErr := ced.Kill(cmd.Context(), &daemon.KillReq{JIDs: []string{jid}})
			if killErr != nil {
				return fmt.Errorf("failed to register bridge job identity: %w (rollback failed for jid %s: %v)", err, jid, killErr)
			}
			return fmt.Errorf("failed to register bridge job identity: %w (rolled back started job %s)", err, jid)
		}

		if attach {
			return ced.Attach(cmd.Context(), &daemon.AttachReq{PID: resp.PID})
		}
		return nil
	},
}

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Bridge job operations",
}

var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Bridge node operations",
}

type bridgeJobRecord struct {
	JID      string `json:"jid"`
	NodeID   string `json:"node_id"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
	Name     string `json:"name"`
}

type bridgeJobsResponse struct {
	Jobs  []bridgeJobRecord `json:"jobs"`
	Total int64             `json:"total"`
}

var jobListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List Bridge jobs from propagator",
	RunE: func(cmd *cobra.Command, args []string) error {
		nodeID, _ := cmd.Flags().GetString("node-id")
		hostname, _ := cmd.Flags().GetString("hostname")
		status, _ := cmd.Flags().GetString("status")
		limit, _ := cmd.Flags().GetInt64("limit")
		offset, _ := cmd.Flags().GetInt64("offset")
		rawJSON, _ := cmd.Flags().GetBool("json")

		params := url.Values{}
		if nodeID != "" {
			params.Set("node_id", nodeID)
		}
		if hostname != "" {
			params.Set("hostname", hostname)
		}
		if status != "" {
			params.Set("status", status)
		}
		params.Set("limit", fmt.Sprintf("%d", limit))
		params.Set("offset", fmt.Sprintf("%d", offset))

		var resp bridgeJobsResponse
		if err := getPropagatorJSON(cmd.Context(), "/v2/bridge/jobs?"+params.Encode(), &resp); err != nil {
			return err
		}

		if rawJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		}

		if len(resp.Jobs) == 0 {
			fmt.Println("No bridge jobs found")
			return nil
		}

		tw := table.NewWriter()
		tw.SetStyle(style.TableStyle)
		tw.SetOutputMirror(os.Stdout)
		tw.SetColumnConfigs([]table.ColumnConfig{{Name: "Node", Align: text.AlignLeft}, {Name: "Status", Align: text.AlignLeft}})
		tw.AppendHeader(table.Row{"JID", "Node", "Hostname", "Status", "Name"})
		for _, j := range resp.Jobs {
			tw.AppendRow(table.Row{j.JID, j.NodeID, j.Hostname, j.Status, j.Name})
		}
		tw.Render()
		fmt.Printf("\nTotal: %d\n", resp.Total)
		return nil
	},
}

type bridgeNodeRecord struct {
	NodeID   string `json:"node_id"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
	LastSeen string `json:"last_seen"`
}

type bridgeNodesResponse struct {
	Nodes []bridgeNodeRecord `json:"nodes"`
	Total int64              `json:"total"`
}

var nodeListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List Bridge nodes from propagator",
	RunE: func(cmd *cobra.Command, args []string) error {
		status, _ := cmd.Flags().GetString("status")
		limit, _ := cmd.Flags().GetInt64("limit")
		offset, _ := cmd.Flags().GetInt64("offset")
		rawJSON, _ := cmd.Flags().GetBool("json")

		params := url.Values{}
		if status != "" {
			params.Set("status", status)
		}
		params.Set("limit", fmt.Sprintf("%d", limit))
		params.Set("offset", fmt.Sprintf("%d", offset))

		var resp bridgeNodesResponse
		if err := getPropagatorJSON(cmd.Context(), "/v2/bridge/nodes?"+params.Encode(), &resp); err != nil {
			return err
		}

		if rawJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		}

		if len(resp.Nodes) == 0 {
			fmt.Println("No bridge nodes found")
			return nil
		}

		tw := table.NewWriter()
		tw.SetStyle(style.TableStyle)
		tw.SetOutputMirror(os.Stdout)
		tw.AppendHeader(table.Row{"Node", "Hostname", "Status", "Last seen"})
		for _, n := range resp.Nodes {
			tw.AppendRow(table.Row{n.NodeID, n.Hostname, n.Status, n.LastSeen})
		}
		tw.Render()
		fmt.Printf("\nTotal: %d\n", resp.Total)
		return nil
	},
}

func getPropagatorJSON(ctx context.Context, path string, out any) error {
	base := strings.TrimRight(config.Global.Connection.URL, "/")
	base = strings.TrimSuffix(base, "/v1")
	base = strings.TrimSuffix(base, "/v2")
	url := base + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if config.Global.Connection.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+config.Global.Connection.AuthToken)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("propagator request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func postPropagatorJSON(ctx context.Context, path string, payload any, out any) error {
	base := strings.TrimRight(config.Global.Connection.URL, "/")
	base = strings.TrimSuffix(base, "/v1")
	base = strings.TrimSuffix(base, "/v2")
	url := base + path

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if config.Global.Connection.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+config.Global.Connection.AuthToken)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("propagator request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out == nil || resp.ContentLength == 0 {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
