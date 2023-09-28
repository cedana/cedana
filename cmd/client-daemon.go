package cmd

import (
	"flag"
	"net/rpc"
	"os"
	"syscall"
	"time"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/services/server"
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
	Run: func(cmd *cobra.Command, args []string) {

		logger := utils.GetLogger()

		cd := api.NewDaemon(stop)
		rpc.Register(cd)

		executable, err := os.Executable()
		if err != nil {
			logger.Fatal().Msg("Could not find cedana executable")
		}

		ctx := &gd.Context{
			PidFileName: "cedana.pid",
			PidFilePerm: 0o644,
			LogFileName: "cedana-daemon.log",
			LogFilePerm: 0o664,
			WorkDir:     "./",
			Umask:       027,
			Args:        []string{executable, "daemon"},
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

		logger.Info().Msgf("daemon started at %s", time.Now().Local())

		go cd.StartDaemon()

		err = gd.ServeSignals()
		if err != nil {
			logger.Fatal().Err(err)
		}

		logger.Info().Msg("daemon terminated")
	},
}

// Here I am introducing a new command to run the daemon in the background with a grpc server
var clientDaemonRPCCmd = &cobra.Command{
	Use:   "daemon-grpc",
	Short: "Start daemon for cedana client. Must be run as root, needed for all other cedana functionality.",
	Run: func(cmd *cobra.Command, args []string) {

		logger := utils.GetLogger()

		if err := server.StartGRPCServer(); err != nil {
			logger.Error().Err(err).Msg("Failed to start gRPC server")
		}

	},
}

func termHandler(sig os.Signal) error {
	stop <- struct{}{}
	if sig == syscall.SIGQUIT {
		<-done
	}
	return gd.ErrStop
}

func init() {
	rootCmd.AddCommand(clientDaemonCmd)
	rootCmd.AddCommand(clientDaemonRPCCmd)
}
