package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/utils"
	"github.com/spf13/cobra"
)

var isK8s bool

var clientDaemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start daemon for cedana client. Must be run as root, needed for all other cedana functionality.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("missing subcommand")
	},
}

var startDaemonCmd = &cobra.Command{
	Use:   "start",
	Short: "Starts the rpc server. To run as a daemon, use the provided script (systemd) or use systemd/sysv/upstart.",
	Run: func(cmd *cobra.Command, args []string) {
		logger := utils.GetLogger()

		if os.Getenv("CEDANA_PROFILING_ENABLED") == "true" {
			logger.Info().Msg("profiling enabled, listening on 6060")
			go startProfiler()
		}

		if os.Getenv("CEDANA_GPU_ENABLED") == "true" {
			err := pullGPUBinary("gpucontroller", "/usr/local/bin/cedana-gpu-controller")
			if err != nil {
				logger.Warn().Err(err).Msg("could not pull gpu controller")
			}

			err = pullGPUBinary("libcedana", "/usr/local/lib/libcedana-gpu.so")
			if err != nil {
				logger.Warn().Err(err).Msg("could not pull libcedana")
			}
		}

		logger.Info().Msgf("daemon started at %s", time.Now().Local())

		startgRPCServer(isK8s)
	},
}

func startgRPCServer(isK8s bool) {
	logger := utils.GetLogger()

	if _, err := api.StartGRPCServer(isK8s); err != nil {
		logger.Error().Err(err).Msg("Failed to start gRPC server")
	}

}

// Used for debugging and profiling only!
func startProfiler() {
	utils.StartPprofServer()
}

func init() {
	rootCmd.AddCommand(clientDaemonCmd)
	clientDaemonCmd.AddCommand(startDaemonCmd)
	startDaemonCmd.Flags().BoolVar(&isK8s, "isK8s", false, "Pass true if Cedana is running within a kubernetes worker node.")
}

func pullGPUBinary(binary string, filePath string) error {
	logger := utils.GetLogger()

	cfg, err := utils.InitConfig()
	if err != nil {
		logger.Err(err).Msg("could not init config")
		return err
	}
	url := "https://" + cfg.Connection.CedanaUrl + "/checkpoint/gpu/" + binary
	logger.Debug().Msgf("pulling %s from %s", binary, url)

	httpClient := &http.Client{}

	req, err := http.NewRequest("GET", url, nil)
	var resp *http.Response
	if err != nil {
		logger.Err(err).Msg("could not create request")
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", "random-user-1234-uuid-think")) // TODO: change to JWT

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
	return nil
}
