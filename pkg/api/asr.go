package api

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
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
	_ "github.com/swarnimarun/cadvisor/container/crio/install"
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

// SystemIdentifier initialization defaults to using rand so that in case we fail to find and update
// with proper machine id it still works
var SystemIdentifier = fmt.Sprintf("%d-%d-%d", rand.Uint64(), rand.Uint64(), rand.Uint64())

func SetupCadvisor(ctx context.Context) (manager.Manager, error) {
	includedMetrics := container.AllMetrics.Difference(ignoreMetrics)
	log.Info().Msgf("enabled metrics: %s", includedMetrics.String())

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

	// TODO: this only works on systemd systems
	// on non systemd systems it might be located at /var/lib/dbus/machine-id
	// but there are no fixed defaults
	// if sysfs is setup then we can also read, /sys/class/dmi/id/product_uuid
	// which has a identifier for product_uuid
	bytes, err := os.ReadFile("/etc/machine-id")
	if err == nil {
		SystemIdentifier = string(bytes)
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
	for name, container := range containers {
		for _, c := range container.Stats {
			info := task.ContainerInfo{
				ContainerName: name,
				DaemonId:      SystemIdentifier,
				// from nanoseconds in uint64 to cputime in float64
				CpuTime:    float64(c.Cpu.Usage.User) / 1000000000.,
				CpuLoadAvg: float64(c.Cpu.LoadAverage) / 1.,
				// from bytes in uin64 to megabytes in float64
				MaxMemory: float64(c.Memory.MaxUsage) / (1024. * 1024.),
				// from bytes in uin64 to megabytes in float64
				CurrentMemory: float64(c.Memory.Usage) / (1024. * 1024.),
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
