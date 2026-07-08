package storage

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/cedana/cedana/pkg/config"
)

type Priority int

const (
	Memory     Priority = 0
	Disk       Priority = 1
	Persistent Priority = 2
)

const (
	DISK_EMPTY_PATH    = "/host/var/lib/kubelet/pods/%v/volumes/kubernetes.io~empty-dir/checkpoint-disk-cache"
	MEM_EMPTY_PATH     = "/host/var/lib/kubelet/pods/%v/volumes/kubernetes.io~empty-dir/checkpoint-ram-cache"
	POD_ID_ENV_VAR     = "CEDANA_HELPER_POD_UID"
	MEM_CACHE_ENV_VAR  = "CEDANA_MEM_CACHE_LIMIT_GB"
	DISK_CACHE_ENV_VAR = "CEDANA_DISK_CACHE_LIMIT_GB"
)

const (
	BYTE = 1.0 << (10 * iota)
	KIBIBYTE
	MEBIBYTE
	GIBIBYTE
)

type Checkpoint struct {
	LayerPriority Priority
	Path          string
	// should have a better way to identify a checkpoint
	Pid  int
	Size int64
}

type Storage struct {
	layers                []*Layer
	inProgressCheckpoints map[int]Checkpoint
	storedCheckpoints     map[int]Checkpoint
}

type Layer struct {
	path      string
	limit     int64
	usedLimit int64
}

func initMemLayer(podID string) (*Layer, error) {
	memPath := fmt.Sprintf(DISK_EMPTY_PATH, podID)
	info, err := os.Stat(memPath)
	if err != nil {
		return nil, fmt.Errorf("could not stat mem path")
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("is not dir")
	}

	limitStr := os.Getenv(MEM_CACHE_ENV_VAR)
	if limitStr == "" {
		return nil, fmt.Errorf("could not get mem limit")
	}

	memLimit, err := strconv.Atoi(limitStr)
	if err != nil {
		return nil, fmt.Errorf("could not get mem limit")
	}

	memLayer := &Layer{}
	memLayer.path = memPath
	memLayer.limit = int64(memLimit) * GIBIBYTE
	return memLayer, nil
}

func initDiskLayer(podID string) (*Layer, error) {
	diskPath := fmt.Sprintf(DISK_EMPTY_PATH, podID)
	info, err := os.Stat(diskPath)
	if err != nil {
		return nil, fmt.Errorf("could not stat disk path")
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("is not dir")
	}

	limitStr := os.Getenv(DISK_CACHE_ENV_VAR)
	if limitStr == "" {
		return nil, fmt.Errorf("could not get disk limit")
	}

	diskLimit, err := strconv.Atoi(limitStr)
	if err != nil {
		return nil, fmt.Errorf("could not get disk limit")
	}

	diskLayer := &Layer{}
	diskLayer.path = diskPath
	diskLayer.limit = int64(diskLimit) * GIBIBYTE
	return diskLayer, nil
}

func initPersistentLayer() *Layer {
	path := config.Global.Checkpoint.Dir
	if !(strings.HasPrefix(path, "cedana://") || strings.HasPrefix(path, "s3://")) {
		return nil
	}

	layer := &Layer{}
	layer.path = path
	layer.limit = math.MaxInt64
	return layer
}

func InitStorage() (*Storage, error) {
	podID := os.Getenv(POD_ID_ENV_VAR)
	if podID == "" {
		return nil, fmt.Errorf("could not get POD ID")
	}

	diskLayer, err := initDiskLayer(podID)
	if err != nil {
		return nil, fmt.Errorf("could not init disk layer")
	}

	memLayer, err := initMemLayer(podID)
	if err != nil {
		return nil, fmt.Errorf("could not init mem layer")
	}

	persistentLayer := initPersistentLayer()

	storage := &Storage{
		layers:                make([]*Layer, 3),
		inProgressCheckpoints: make(map[int]Checkpoint),
		storedCheckpoints:     make(map[int]Checkpoint),
	}
	storage.layers[Memory] = memLayer
	storage.layers[Disk] = diskLayer
	if persistentLayer != nil {
		storage.layers[Persistent] = persistentLayer
	}

	return storage, nil
}
