package cmd

// This file contains all the daemon-related commands when starting `cedana daemon ...`

import (
	"fmt"
	"os"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start daemon for cedana client. Must be run as root, needed for all other cedana functionality.",
}

var cudaVersions = map[string]string{
	"11.8": "cuda11_8",
	"12.1": "cuda12_1",
	"12.2": "cuda12_2",
	"12.4": "cuda12_4",
}

var startDaemonCmd = &cobra.Command{
	Use:   "start",
	Short: "Starts the rpc server. To run as a daemon, use the provided script (systemd) or use systemd/sysv/upstart.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)

		if os.Getuid() != 0 {
			return fmt.Errorf("daemon must be run as root")
		}

		_, err := utils.InitOtel(cmd.Context(), cmd.Parent().Version)
		if err != nil {
			logger.Warn().Err(err).Msg("Failed to initialize otel")
			return err
		}

		logger.Info().Msg("otel initialized")

		if viper.GetBool("profiling_enabled") {
			go startProfiler()
		}
		gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)
		// defaults to 11_8, this continues if --cuda is not specified
		cudaVersion, _ := cmd.Flags().GetString(cudaVersionFlag)
		if _, ok := cudaVersions[cudaVersion]; !ok {
			err = fmt.Errorf("invalid cuda version %s, must be one of %v", cudaVersion, cudaVersions)
			logger.Error().Err(err).Msg("invalid cuda version")
			return err
		}

		logger.Info().Msgf("starting daemon version %s", rootCmd.Version)

		port, err := cmd.Flags().GetUint64(portFlag)
		if err != nil {
			port = 8080
		}
		err = api.StartServer(ctx, &api.ServeOpts{GPUEnabled: gpuEnabled, CUDAVersion: cudaVersions[cudaVersion], Port: port})
		if err != nil {
			logger.Error().Err(err).Msgf("stopping daemon")
			return err
		}

		return nil
	},
}

var checkDaemonCmd = &cobra.Command{
	Use:   "check",
	Short: "Check if daemon is running and healthy",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)

		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Err(err).Msg("error creating client")
			return err
		}

		defer cts.Close()

		// regular health check
		healthy, err := cts.HealthCheck(cmd.Context())
		if err != nil {
			logger.Error().Err(err).Msg("health check failed")
			return err
		}

		logger.Info().Msgf("health check returned: %v", healthy)

		// Detailed health check. Need to grab uid and gid to start
		// controller properly and with the right perms.
		var uid int32
		var gid int32
		var groups []int32 = []int32{}

		uid = int32(os.Getuid())
		gid = int32(os.Getgid())
		groups_int, err := os.Getgroups()
		if err != nil {
			logger.Error().Err(err).Msg("error getting user groups")
			return err
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
			logger.Error().Err(err).Msg("health check failed")
			return err
		}

		logger.Info().Msgf("health check output: %v", resp)

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
	startDaemonCmd.Flags().String(cudaVersionFlag, "11.8", "cuda version to use")
	startDaemonCmd.Flags().Uint64P(portFlag, "p", 8080, "port to use for daemon")
}
