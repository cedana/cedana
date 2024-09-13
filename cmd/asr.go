package cmd

import (
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/api"
	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var asrCmd = &cobra.Command{
	Use:   "asr",
	Short: "Automatic suspend resume",
}

var asrStartCmd = &cobra.Command{
	Use:     "start",
	Short:   "Start and poll the automatic suspend resume",
	Aliases: []string{"s"},
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Info().Msg("Start automatic suspend resume")
		ctx := cmd.Context()
		// start the server

		cedanaURL := viper.GetString("connection.cedana_url")
		if cedanaURL == "" {
			cedanaURL = "unset"
		}

		go func() {
			err := api.StartServer(ctx, &api.ServeOpts{
				GPUEnabled:   false,
				CUDAVersion:  cudaVersions["12.2"],
				VSOCKEnabled: vsockEnabledFlag,
				CedanaURL:    cedanaURL,
				// TODO(swarnimarun): allow flag to customize the port
				GrpcPort: 8080,
			})
			if err != nil {
				log.Fatal().Err(err).Msgf("failed to start the daemon server, cleaning up")
			}
		}()

		var wg sync.WaitGroup
		wg.Add(1)

		// start the poller
		go func() {
			time.Sleep(5 * time.Second)
			defer wg.Done()
			cts, err := services.NewClient()
			if err != nil {
				log.Error().Msgf("Error creating client: %v", err)
				return
			}
			defer cts.Close()

			log.Info().Msg("we started polling...")
			for {
				conts, err := cts.GetContainerInfo(ctx, &task.ContainerInfoRequest{})
				if err != nil {
					log.Error().Msgf("error getting info: %v", err)
					return
				}
				log.Info().Msgf("containers")
				for _, cont := range conts.Containers {
					log.Info().Msgf("\t container(%s): cputime: %fs    %f bytes", cont.ContainerName, cont.CpuTime, cont.CurrentMemory)
				}
				time.Sleep(1 * time.Second)
			}
		}()

		wg.Wait()

		return nil
	},
}

func init() {
	asrCmd.AddCommand(asrStartCmd)
	rootCmd.AddCommand(asrCmd)
}
