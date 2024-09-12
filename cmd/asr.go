package cmd

import (
	"fmt"
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

		var err error

		gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)
		// defaults to 11_8, this continues if --cuda is not specified
		cudaVersion, _ := cmd.Flags().GetString(cudaVersionFlag)
		if _, ok := cudaVersions[cudaVersion]; !ok {
			err = fmt.Errorf("invalid cuda version %s, must be one of %v", cudaVersion, cudaVersions)
			log.Error().Err(err).Msg("invalid cuda version")
			return err
		}
		vsockEnabled, _ := cmd.Flags().GetBool(vsockEnabledFlag)
		port, _ := cmd.Flags().GetUint32(portFlag)
		metricsEnabled, _ := cmd.Flags().GetBool(metricsEnabledFlag)
		jobServiceEnabled, _ := cmd.Flags().GetBool(jobServiceFlag)

		cedanaURL := viper.GetString("connection.cedana_url")
		if cedanaURL == "" {
			cedanaURL = "unset"
		}

		go func() {
			err := api.StartServer(ctx, &api.ServeOpts{
				GPUEnabled:        gpuEnabled,
				CUDAVersion:       cudaVersions[cudaVersion],
				VSOCKEnabled:      vsockEnabled,
				CedanaURL:         cedanaURL,
				MetricsEnabled:    metricsEnabled,
				JobServiceEnabled: jobServiceEnabled,
				Port:              port,
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
			cts, err := services.NewClient(port)
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
	asrCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "start with GPU support")
	asrCmd.Flags().Bool(vsockEnabledFlag, false, "start with vsock support")
	asrCmd.Flags().String(cudaVersionFlag, "11.8", "cuda version to use")
	asrCmd.Flags().BoolP(metricsEnabledFlag, "m", false, "enable metrics")
	asrCmd.Flags().Bool(jobServiceFlag, false, "enable job service")

	rootCmd.AddCommand(asrCmd)
}
