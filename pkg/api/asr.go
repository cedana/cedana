package api

import (
	"context"
	"strings"
	"time"

	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/rs/zerolog/log"
	"github.com/swarnimarun/cadvisor/cache/memory"
	v1 "github.com/swarnimarun/cadvisor/info/v1"
	"github.com/swarnimarun/cadvisor/manager"
	"github.com/swarnimarun/cadvisor/storage"
	"github.com/swarnimarun/cadvisor/utils/sysfs"

	"github.com/swarnimarun/cadvisor/container"
)

// Time is in milliseconds
const CONTAINER_INFO_STREAMING_RATE int = 1000

func SetupCadvisor(ctx context.Context) (manager.Manager, error) {
	includedMetrics := container.MetricSet{
		container.MemoryUsageMetrics: {},
		container.CpuLoadMetrics:     {},
		container.CpuUsageMetrics:    {},
	}
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

func (s *service) GetContainerInfo(_ *task.ContainerInfoRequest) (*task.ContainersInfo, error) {
	// infos := []task.ContainerInfo{
	// 	{
	// 		CpuTime:       0,
	// 		CpuLoadAvg:    0,
	// 		MaxMemory:     0,
	// 		CurrentMemory: 0,
	// 		NetworkIO:     0,
	// 		DiskIO:        0,
	// 	},
	// }
	containers, err := s.cadvisorManager.AllContainerdContainers(&v1.ContainerInfoRequest{
		NumStats: 1,
	})
	ci := task.ContainersInfo{}
	for container := range containers {
		for _, c := range container.Stats {
			info := task.ContainerInfo{
				CpuTime:       c.Cpu.Usage.User,
				CpuLoadAvg:    c.Cpu.LoadAverage,
				MaxMemory:     c.Memory.Max,
				CurrentMemory: c.Memory.Usage,
				NetworkIO:     0,
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
