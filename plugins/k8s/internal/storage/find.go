package storage

import (
	"fmt"
	"log"
	"os"
)

const (
	DISK_EMPTY_PATH = "/host/var/lib/kubelet/pods/%v/volumes/kubernetes.io~empty-dir/checkpoint-disk-cache"
	MEM_EMPTY_PATH  = "/host/var/lib/kubelet/pods/%v/volumes/kubernetes.io~empty-dir/checkpoint-ram-cache"
	POD_ID_ENV_VAR  = "CEDANA_HELPER_POD_UID"
)

// Assume POD_ID is available at the POD_ID_ENV_VAR
// and the host file system is mounted at /host
func FindDiskEmptyDir() (string, error) {
	root, err := os.Readlink("/proc/self/root")
	if err != nil {
		fmt.Printf("could not open proc root file, err: %v\n", err)
	} else {
		log.Printf("root: %s\n", root)
	}

	mntNs, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		fmt.Printf("could not open proc root file, err: %v\n", err)
	} else {
		log.Printf("mntNs: %s\n", mntNs)
	}

	podID := os.Getenv(POD_ID_ENV_VAR)
	if podID == "" {
		return "", fmt.Errorf("could not get POD ID")
	}

	log.Printf("BHAVIK: found pod id: %v\n", podID)
	path := fmt.Sprintf(DISK_EMPTY_PATH, podID)
	return path, nil
}
