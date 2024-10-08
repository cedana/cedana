package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
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
	containerd_plugin "github.com/swarnimarun/cadvisor/container/containerd/install"
	crio_plugin "github.com/swarnimarun/cadvisor/container/crio/install"
)

// Metrics to be ignored.
// Tcp metrics are ignored by default.
var ignoreMetrics = container.MetricSet{
	container.MemoryNumaMetrics:              struct{}{},
	container.NetworkTcpUsageMetrics:         struct{}{},
	container.NetworkUdpUsageMetrics:         struct{}{},
	container.NetworkAdvancedTcpUsageMetrics: struct{}{},
	container.ProcessSchedulerMetrics:        struct{}{},
	container.HugetlbUsageMetrics:            struct{}{},
	container.ReferencedMemoryMetrics:        struct{}{},
	container.CPUTopologyMetrics:             struct{}{},
	container.ResctrlMetrics:                 struct{}{},
	container.CPUSetMetrics:                  struct{}{},
}

// SystemIdentifier initialization defaults to using rand so that in case we fail to find and update
// with proper machine id it still works
var SystemIdentifier = fmt.Sprintf("%d-%d-%d", rand.Uint64(), rand.Uint64(), rand.Uint64())

func SetupCadvisor(ctx context.Context) (manager.Manager, error) {
	includedMetrics := container.AllMetrics.Difference(ignoreMetrics)
	log.Info().Msgf("enabled metrics: %s", includedMetrics.String())

	memoryStorage, err := NewMemoryStorage()
	if err != nil {
		log.Error().Msgf("Failed to initialize storage driver: %s", err)
		return nil, err
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
		log.Error().Msgf("Failed to create a manager: %s", err)
		return nil, err
	}

	// Start the manager.
	if err := resourceManager.Start(); err != nil {
		log.Error().Msgf("Failed to start manager: %v", err)
		return nil, err
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
	if s.cadvisorManager == nil {
		return nil, fmt.Errorf("cadvisor manager not enabled in daemon")
	}

	containerdService := containerd_plugin.Success
	crioService := crio_plugin.Success

	var containers map[string]v1.ContainerInfo
	var err error

	if containerdService && crioService {
		containers, err = s.cadvisorManager.AllContainerdContainers(&v1.ContainerInfoRequest{
			NumStats: 1,
		})
		ccontainers, err := s.cadvisorManager.AllCrioContainers(&v1.ContainerInfoRequest{
			NumStats: 1,
		})
		if err != nil {
			return nil, err
		}
		for k, v := range ccontainers {
			// |> do not duplicate names.
			// they are unique existing in both likely is a case of bad setup or
			// due to lack of cleanup
			if _, f := containers[k]; !f {
				containers[k] = v
			} else {
				log.Debug().Msg("found duplicate container name between containerd and crio")
			}
		}
	} else if containerdService {
		containers, err = s.cadvisorManager.AllContainerdContainers(&v1.ContainerInfoRequest{
			NumStats: 1,
		})
	} else if crioService {
		containers, err = s.cadvisorManager.AllCrioContainers(&v1.ContainerInfoRequest{
			NumStats: 1,
		})
	}
	if err != nil {
		return nil, err
	}

	ci := task.ContainersInfo{}
	for name, container := range containers {
		var labels string
		labelsJson, err := json.Marshal(container.Spec.Labels)
		if err == nil {
			labels = string(labelsJson)
		} else {
			log.Info().Msgf("error marshalling labels: %v", err)
		}

		for _, c := range container.Stats {
			info := task.ContainerInfo{
				CpuTime:           float64(c.Cpu.Usage.Total) / 1000000000.,
				FilesystemIoTime:  cumulativeFsTime(c.Filesystem),
				AcceleratorMemory: cumulativeAcceleratorsMem(c.Accelerators),
				CurrentMemory:     float64(c.Memory.Usage) / (1024. * 1024.),
				NetworkIO:         float64(c.Network.RxBytes + c.Network.TxBytes),
				DiskIO:            cumulativeDiskIoTime(c.DiskIo.IoTime),
				ContainerName:     name,
				Processes:         strconv.FormatUint(c.Processes.ProcessCount, 10),
				Labels:            labels,
				Image:             container.Spec.Image,
			}
			ci.Containers = append(ci.Containers, &info)
		}
	}
	return &ci, nil
}

func cumulativeAcceleratorsMem(stats []v1.AcceleratorStats) float64 {
	sum := 0.0
	for _, s := range stats {
		// memory in megabytes
		sum += float64(s.MemoryUsed) / (1024. * 1024.)
	}
	return sum
}

func cumulativeFsTime(stats []v1.FsStats) float64 {
	sum := 0.0
	for _, s := range stats {
		// time in seconds
		sum += float64(s.IoTime) / 1000
	}
	return sum
}

func cumulativeDiskIoTime(stats []v1.PerDiskStats) float64 {
	return 0
}

var (
	storageDriver   = string("")
	storageDuration = 2 * time.Minute
)

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
