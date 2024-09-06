package api

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/rs/zerolog/log"
	"github.com/swarnimarun/cadvisor/cache/memory"
	"github.com/swarnimarun/cadvisor/storage"

	"github.com/swarnimarun/cadvisor/container"
	v1 "github.com/swarnimarun/cadvisor/info/v1"
	"github.com/swarnimarun/cadvisor/manager"
	"github.com/swarnimarun/cadvisor/utils/sysfs"

	// Register container providers
	_ "github.com/swarnimarun/cadvisor/container/containerd/install"
)

var (
	// Metrics to be ignored.
	// Tcp metrics are ignored by default.
	ignoreMetrics = container.MetricSet{
		container.MemoryNumaMetrics:              struct{}{},
		container.NetworkTcpUsageMetrics:         struct{}{},
		container.NetworkUdpUsageMetrics:         struct{}{},
		container.NetworkAdvancedTcpUsageMetrics: struct{}{},
		container.ProcessSchedulerMetrics:        struct{}{},
		container.ProcessMetrics:                 struct{}{},
		container.HugetlbUsageMetrics:            struct{}{},
		container.ReferencedMemoryMetrics:        struct{}{},
		container.CPUTopologyMetrics:             struct{}{},
		container.ResctrlMetrics:                 struct{}{},
		container.CPUSetMetrics:                  struct{}{},
	}
)

func SetupCadvisor(ctx context.Context) (manager.Manager, error) {
	// includedMetrics := container.MetricSet{
	// 	container.MemoryUsageMetrics: {},
	// 	container.CpuLoadMetrics:     {},
	// 	container.CpuUsageMetrics:    {},
	// }
	includedMetrics := container.AllMetrics.Difference(ignoreMetrics)

	log.Info().Msgf("enabled metrics: %s", includedMetrics.String())
	// setMaxProcs()

	memoryStorage, err := NewMemoryStorage()
	if err != nil {
		log.Fatal().Msgf("Failed to initialize storage driver: %s", err)
	}

	sysFs := sysfs.NewRealSysFs()

	resourceManager, err := manager.New(
		memoryStorage,
		sysFs,
		manager.HousekeepingConfigFlags,
		includedMetrics,
		strings.Split("", ","),
		strings.Split("", ","),
	)
	if err != nil {
		log.Fatal().Msgf("Failed to create a manager: %s", err)
	}

	// Start the manager.
	if err := resourceManager.Start(); err != nil {
		log.Fatal().Msgf("Failed to start manager: %v", err)
	}

	return resourceManager, nil
}

func (s *service) GetContainerInfo(ctx context.Context, _ *task.ContainerInfoRequest) (*task.ContainersInfo, error) {
	containers, err := s.cadvisorManager.AllContainerdContainers(&v1.ContainerInfoRequest{
		NumStats: 1,
	})
	if err != nil {
		return nil, err
	}
	ci := task.ContainersInfo{}
	for _, container := range containers {
		for _, c := range container.Stats {
			info := task.ContainerInfo{
				CpuTime:       float64(c.Cpu.Usage.User) / 1000000000.,
				CpuLoadAvg:    float64(c.Cpu.LoadAverage) / 1.,
				MaxMemory:     float64(c.Memory.MaxUsage) / 1.,
				CurrentMemory: float64(c.Memory.Usage) / 1.,
				NetworkIO:     float64(c.Network.RxBytes+c.Network.TxBytes) / 1.,
				DiskIO:        0,
			}
			ci.Containers = append(ci.Containers, &info)
		}
	}
	return &ci, nil
}

var storageDriver = string("")
var storageDuration = 2 * time.Minute

func NewMemoryStorage() (*memory.InMemoryCache, error) {
	backendStorages := []storage.StorageDriver{}
	for _, driver := range strings.Split(storageDriver, ",") {
		if driver == "" {
			continue
		}
		storage, err := storage.New(driver)
		if err != nil {
			return nil, err
		}
		backendStorages = append(backendStorages, storage)
		log.Info().Msgf("Using backend storage type %q", driver)
	}
	log.Info().Msgf("Caching stats in memory for %v", storageDuration)
	return memory.New(storageDuration, backendStorages), nil
}

func setMaxProcs() {
	// TODO(vmarmol): Consider limiting if we have a CPU mask in effect.
	// Allow as many threads as we have cores unless the user specified a value.
	var numProcs int
	numProcs = runtime.NumCPU()
	runtime.GOMAXPROCS(numProcs)

	// Check if the setting was successful.
	actualNumProcs := runtime.GOMAXPROCS(0)
	if actualNumProcs != numProcs {
		log.Warn().Msgf("Specified max procs of %v but using %v", numProcs, actualNumProcs)
	}
}

func installSignalHandler(containerManager manager.Manager) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Block until a signal is received.
	go func() {
		sig := <-c
		if err := containerManager.Stop(); err != nil {
			log.Error().Msgf("Failed to stop container manager: %v", err)
		}
		log.Info().Msgf("Exiting given signal: %v", sig)
		os.Exit(0)
	}()
}
