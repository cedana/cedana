package storage

import (
	"fmt"
	"os"
)

const (
	DISK_EMPTY_PATH = "/host/var/lib/kubelet/pods/%v/volumes/kubernetes.io~empty-dir/checkpoint-disk-cache"
	MEM_EMPTY_PATH  = "/host/var/lib/kubelet/pods/%v/volumes/kubernetes.io~empty-dir/checkpoint-ram-cache"
	POD_ID_ENV_VAR  = "CEDANA_HELPER_POD_UID"
)

// Assume POD_ID is available at the POD_ID_ENV_VAR
func FindDiskEmptyDir() (string, error) {
	podID := os.Getenv(POD_ID_ENV_VAR)
	if podID == "" {
		return "", fmt.Errorf("could not get POD ID")
	}

	path := fmt.Sprintf(DISK_EMPTY_PATH, podID)
	return path, nil
}
