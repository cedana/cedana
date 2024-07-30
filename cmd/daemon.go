package cmd

// This file contains all the daemon-related commands when starting `cedana daemon ...`

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

		if viper.GetBool("profiling_enabled") {
			go startProfiler()
		}
		gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)
		if gpuEnabled {
			// defaults to 11_8, this continues if --cuda is not specified
			cudaVersion, _ := cmd.Flags().GetString(cudaVersionFlag)
			if _, ok := cudaVersions[cudaVersion]; !ok {
				err = fmt.Errorf("invalid cuda version %s, must be one of %v", cudaVersion, cudaVersions)
				logger.Error().Err(err).Msg("invalid cuda version")
				return err
			}

			if viper.GetString("gpu_controller_path") == "" {
				err = pullGPUBinary(ctx, utils.GpuControllerBinaryName, utils.GpuControllerBinaryPath, cudaVersions[cudaVersion])
				if err != nil {
					logger.Error().Err(err).Msg("could not pull gpu controller")
					return err
				}
			} else {
				logger.Debug().Msgf("using gpu controller at %s", viper.GetString("gpu_controller_path"))
			}

			if viper.GetString("gpu_shared_lib_path") == "" {
				err = pullGPUBinary(ctx, utils.GpuSharedLibName, utils.GpuSharedLibPath, cudaVersions[cudaVersion])
				if err != nil {
					logger.Error().Err(err).Msg("could not pull libcedana")
					return err
				}
			} else {
				logger.Debug().Msgf("using gpu shared lib at %s", viper.GetString("gpu_shared_lib_path"))
			}
		}

		logger.Info().Msgf("starting daemon version %s", rootCmd.Version)

		err = api.StartServer(ctx)
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
		var uid uint32
		var gid uint32
		var groups []uint32 = []uint32{}

		uid = uint32(os.Getuid())
		gid = uint32(os.Getgid())
		groups_int, err := os.Getgroups()
		if err != nil {
			logger.Error().Err(err).Msg("error getting user groups")
			return err
		}
		for _, g := range groups_int {
			groups = append(groups, uint32(g))
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
}

type pullGPUBinaryRequest struct {
	CudaVersion string `json:"cuda_version"`
}

func pullGPUBinary(ctx context.Context, binary string, filePath string, version string) error {
	logger := ctx.Value("logger").(*zerolog.Logger)
	_, err := os.Stat(filePath)
	if err == nil {
		logger.Debug().Str("Path", filePath).Msgf("GPU binary exists. Delete existing binary to download another supported cuda version.")
		// TODO NR - check version and checksum of binary?
		return nil
	}
	logger.Debug().Msgf("pulling gpu binary %s for cuda version %s", binary, version)

	url := viper.GetString("connection.cedana_url") + "/checkpoint/gpu/" + binary
	logger.Debug().Msgf("pulling %s from %s", binary, url)

	httpClient := &http.Client{}

	body := pullGPUBinaryRequest{
		CudaVersion: version,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		logger.Err(err).Msg("could not marshal request body")
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		logger.Err(err).Msg("gpu binary get request failed")
		return err
	}
	defer resp.Body.Close()

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0755)
	if err == nil {
		err = os.Chmod(filePath, 0755)
	}
	if err != nil {
		logger.Err(err).Msg("could not create file")
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		logger.Err(err).Msg("could not read file from response")
		return err
	}
	logger.Debug().Msgf("%s downloaded to %s", binary, filePath)
	return err
}
