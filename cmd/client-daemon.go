package cmd

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/utils"
	gd "github.com/sevlyar/go-daemon"
	"github.com/spf13/cobra"
)

var stop = make(chan struct{})
var done = make(chan struct{})
var daemonSignal = flag.String("s", "", "")

var clientDaemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start daemon for cedana client. Must be run as root, needed for all other cedana functionality.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("missing subcommand")
	},
}

var startDaemonCmd = &cobra.Command{
	Use:   "start",
	Short: "Start daemon for cedana client. Must be run as root, needed for all other cedana functionality.",
	Run: func(cmd *cobra.Command, args []string) {

		logger := utils.GetLogger()

		executable, err := os.Executable()
		if err != nil {
			logger.Fatal().Msg("Could not find cedana executable")
		}

		ctx := &gd.Context{
			PidFileName: "/run/cedana.pid",
			PidFilePerm: 0o664,
			LogFileName: "/var/log/cedana-daemon.log",
			LogFilePerm: 0o664,
			WorkDir:     "./",
			Umask:       027,
			Args:        []string{executable, "daemon", "start"},
		}

		gd.AddCommand(gd.StringFlag(daemonSignal, "stop"), syscall.SIGTERM, termHandler)

		d, err := ctx.Reborn()
		if err != nil {
			logger.Err(err).Msg("could not start daemon")
		}

		if d != nil {
			return
		}

		defer ctx.Release()

		if os.Getenv("CEDANA_PROFILING_ENABLED") == "true" {
			logger.Info().Msg("profiling enabled, listening on 6060")
			go startProfiler()
		}

		if os.Getenv("CEDANA_GPU_ENABLED") == "true" {
			err = pullGPUBinary("gpucontroller", "./gpu-controller")
			if err != nil {
				logger.Warn().Err(err).Msg("could not pull gpu controller")
			}

			err = pullGPUBinary("libcedana", "./libcedana-gpu.so")
			if err != nil {
				logger.Warn().Err(err).Msg("could not pull libcedana")
			}
		}

		logger.Info().Msgf("daemon started at %s", time.Now().Local())

		go startgRPCServer()

		err = gd.ServeSignals()
		if err != nil {
			logger.Fatal().Err(err)
		}

		logger.Info().Msg("daemon terminated")
	},
}

var stopDaemonCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop cedana client daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		// kill -9 daemon
		// read from PID file
		pidFile, err := os.ReadFile("/run/cedana.pid")
		if err != nil {
			return err
		}
		pid, err := strconv.Atoi(string(pidFile))
		if err != nil {
			return err
		}

		err = syscall.Kill(pid, syscall.SIGKILL)
		if err != nil {
			return err
		}

		err = os.Remove("/run/cedana.pid")
		if err != nil {
			return err
		}

		return nil
	},
}

var statusDaemonCmd = &cobra.Command{
	Use:   "status",
	Short: "Status cedana client daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		pidFile, err := os.ReadFile("/run/cedana.pid")
		if err != nil {
			return err
		}
		_, err = strconv.Atoi(string(pidFile))
		if err != nil {
			return err
		}

		return nil

	},
}

func termHandler(sig os.Signal) error {
	stop <- struct{}{}
	if sig == syscall.SIGTERM || sig == syscall.SIGQUIT {
		<-done
	}
	return gd.ErrStop
}

func startgRPCServer() {
	logger := utils.GetLogger()

	if _, err := api.StartGRPCServer(); err != nil {
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
	clientDaemonCmd.AddCommand(stopDaemonCmd)
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
