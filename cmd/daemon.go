package cmd

// This file contains all the daemon-related commands when starting `cedana daemon ...`

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start daemon for cedana client. Must be run as root, needed for all other cedana functionality.",
}

var startDaemonCmd = &cobra.Command{
	Use:   "start",
	Short: "Starts the rpc server. To run as a daemon, use the provided script (systemd) or use systemd/sysv/upstart.",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)

		if os.Getuid() != 0 {
			logger.Error().Msg("daemon must be run as root")
			return
		}

		stopOtel, err := utils.InitOtel(cmd.Context(), cmd.Parent().Version)
		if err != nil {
			logger.Warn().Err(err).Msg("Failed to initialize otel")
		}
		defer stopOtel(ctx)

		if viper.GetBool("profiling_enabled") {
			go startProfiler()
		}
		gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)
		if gpuEnabled {
			err := pullGPUBinary(ctx, utils.GpuControllerBinaryName, utils.GpuControllerBinaryPath)
			if err != nil {
				logger.Error().Err(err).Msg("could not pull gpu controller")
				return
			}

			err = pullGPUBinary(ctx, utils.GpuSharedLibName, utils.GpuSharedLibPath)
			if err != nil {
				logger.Error().Err(err).Msg("could not pull libcedana")
				return
			}
		}

		logger.Info().Msgf("starting daemon version %s", rootCmd.Version)

		err = api.StartServer(ctx)
		if err != nil {
			logger.Error().Err(err).Msgf("stopping daemon")
		}
	},
}

// Used for debugging and profiling only!
func startProfiler() {
	utils.StartPprofServer()
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(startDaemonCmd)
	startDaemonCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "start daemon with GPU support")
}

func pullGPUBinary(ctx context.Context, binary string, filePath string) error {
	logger := ctx.Value("logger").(*zerolog.Logger)
	_, err := os.Stat(filePath)
	if err == nil {
		logger.Debug().Msgf("binary exists at %s, doing nothing", filePath)
		// file exists, do nothing.
		// TODO NR - check version of binary
		return nil
	}

	url := "https://" + viper.GetString("connection.cedana_url") + "/checkpoint/gpu/" + binary
	logger.Debug().Msgf("pulling %s from %s", binary, url)

	httpClient := &http.Client{}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	var resp *http.Response
	if err != nil {
		logger.Err(err).Msg("could not create request")
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))

	resp, err = httpClient.Do(req)
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
	logger.Debug().Msgf("%s downloaded", binary)
	return err
}
