package cmd

// This file contains all the daemon-related commands when starting `cedana daemon ...`

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/cedana/cedana/pkg/api"
	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start daemon for cedana client. Must be run as root, needed for all other cedana functionality.",
}

var (
	DEFAULT_PORT      uint32 = 8080
	ASR_POLL_INTERVAL        = 60 * time.Second
)

var startDaemonCmd = &cobra.Command{
	Use:   "start",
	Short: "Starts the rpc server. To run as a daemon, use the provided script (systemd) or use systemd/sysv/upstart.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if os.Getuid() != 0 {
			return fmt.Errorf("daemon must be run as root")
		}

		if viper.GetBool("profiling_enabled") {
			go startProfiler()
		}

		var err error

		gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)
		vsockEnabled, _ := cmd.Flags().GetBool(vsockEnabledFlag)
		port, _ := cmd.Flags().GetUint32(portFlag)
		metricsEnabled, _ := cmd.Flags().GetBool(metricsEnabledFlag)
		jobServiceEnabled, _ := cmd.Flags().GetBool(jobServiceFlag)

		cedanaURL := viper.GetString("connection.cedana_url")
		if cedanaURL == "" {
			log.Warn().Msg("CEDANA_URL or CEDANA_AUTH_TOKEN unset, certain features may not work as expected.")
			cedanaURL = "unset"
		}

		log.Info().Str("version", rootCmd.Version).Msg("starting daemon")

		// poll for otel signoz logging
		otel_enabled := viper.GetBool("otel_enabled")
		if otel_enabled {
			_, err := utils.InitOtel(ctx, rootCmd.Version)
			if err != nil {
				log.Warn().Err(err).Msg("Failed to initialize otel")
				return err
			}
		} else {
			utils.InitOtelNoop()
		}
		if metricsEnabled {
			pollForAsrMetricsReporting(ctx, port)
		}

		err = api.StartServer(ctx, &api.ServeOpts{
			GPUEnabled:        gpuEnabled,
			VSOCKEnabled:      vsockEnabled,
			CedanaURL:         cedanaURL,
			MetricsEnabled:    metricsEnabled,
			JobServiceEnabled: jobServiceEnabled,
			Port:              port,
		})
		if err != nil {
			log.Error().Err(err).Msgf("stopping daemon")
			return err
		}

		return nil
	},
}

func getenv(k, d string) string {
	if s, f := os.LookupEnv(k); f {
		return s
	}
	return d
}

func gcloudAdcSetup(ctx context.Context) error {
	adcPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if adcPath == "" {
		// set env if not present
		// default to root /gcloud-credentials.json
		adcPath = "/gcloud-credentials.json"
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", adcPath)
	}
	if _, err := os.Stat(adcPath); err == nil {
		// already present skip
		return nil
	}
	cedanaURL := viper.GetString("connection.cedana_url")
	url := cedanaURL + "/k8s/gcloud/serviceaccount"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	err = os.WriteFile(adcPath, bytes, 0600)
	if err != nil {
		return err
	}
	return nil
}

func pollForAsrMetricsReporting(ctx context.Context, port uint32) {
	// polling for ASR
	go func() {
		// setup GCLOUD_JSON
		err := gcloudAdcSetup(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup gcloud ADC, disabling reporting")
			return
		}
		// end
		log.Info().Msg("start pushing asr metrics")
		client, err := pubsub.NewClient(ctx, getenv("GOOGLE_CLOUD_PROJECT", "prod-data-438318"))
		if err != nil {
			log.Error().Msgf("Failed to create Pub/Sub client: %v", err)
			return
		}
		defer client.Close()
		manager, err := api.SetupCadvisor(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup cadvisor")
			return
		}

		macAddr, _ := utils.GetMACAddress()
		hostname, _ := os.Hostname()
		v, _ := mem.VirtualMemory()
		pmem := fmt.Sprintf("%d", v.Total/(1024*1024*1024)) // in GB
		url := viper.GetString("connection.cedana_url")

		topic := client.Topic("asr-metrics")
		time.Sleep(10 * time.Second)
		for {
			conts, err := api.GetContainerInfo(ctx, manager)
			if err != nil {
				log.Error().Msgf("error getting info: %v", err)
				return
			}
			b, err := json.Marshal(conts)
			// Publish a message
			result := topic.Publish(ctx, &pubsub.Message{
				Data: b,
				Attributes: map[string]string{
					"mac":      macAddr,
					"hostname": hostname,
					"mem":      pmem,
					"url":      url,
				},
			})
			// Get the server-assigned message ID
			id, err := result.Get(ctx)
			if err != nil {
				log.Error().Msgf("Failed to publish message: %v", err)
			}
			log.Info().Msgf("Published message with ID: %v\n", id)
			time.Sleep(ASR_POLL_INTERVAL)
		}
	}()
}

var checkDaemonCmd = &cobra.Command{
	Use:   "check",
	Short: "Check if daemon is running and healthy",
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetUint32(portFlag)

		cts, err := services.NewClient(port)
		if err != nil {
			log.Error().Err(err).Msg("error creating client")
			return err
		}

		defer cts.Close()

		// regular health check
		healthy, err := cts.HealthCheck(cmd.Context())
		if err != nil {
			return err
		}
		if !healthy {
			return fmt.Errorf("health check failed")
		}

		// Detailed health check. Need to grab uid and gid to start
		// controller properly and with the right perms.
		var uid int32
		var gid int32
		var groups []int32 = []int32{}

		uid = int32(os.Getuid())
		gid = int32(os.Getgid())
		groups_int, err := os.Getgroups()
		if err != nil {
			return fmt.Errorf("error getting user groups: %v", err)
		}
		for _, g := range groups_int {
			groups = append(groups, int32(g))
		}

		req := &task.DetailedHealthCheckRequest{
			UID:    uid,
			GID:    gid,
			Groups: groups,
		}

		resp, err := cts.DetailedHealthCheck(cmd.Context(), req)
		if err != nil {
			return fmt.Errorf("health check failed: %v", err)
		}

		if len(resp.UnhealthyReasons) > 0 {
			return fmt.Errorf("health failed with reasons: %v", resp.UnhealthyReasons)
		}

		fmt.Println("All good.")
		fmt.Println("Cedana version: ", rootCmd.Version)
		fmt.Println("CRIU version: ", resp.HealthCheckStats.CriuVersion)
		if resp.HealthCheckStats.GPUHealthCheck != nil {
			prettyJson, err := json.MarshalIndent(resp.HealthCheckStats.GPUHealthCheck, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println("GPU support: ", string(prettyJson))
		}

		return nil
	},
}

// Used for debugging and profiling only!
func startProfiler() {
	utils.StartPprofServer()
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(startDaemonCmd)
	daemonCmd.AddCommand(checkDaemonCmd)
	startDaemonCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "start daemon with GPU support")
	startDaemonCmd.Flags().Bool(vsockEnabledFlag, false, "start daemon with vsock support")
	startDaemonCmd.Flags().BoolP(metricsEnabledFlag, "m", false, "enable metrics")
	startDaemonCmd.Flags().Bool(jobServiceFlag, false, "enable job service")
}
