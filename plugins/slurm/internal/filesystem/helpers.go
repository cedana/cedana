//go:build linux

package filesystem

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (

	// Lists the private mounts whose contents are in the dump, each as a '<prefix>-<n>.tar'
	PRIVATE_MOUNTS_FILE   = "private_mounts.json"
	PRIVATE_MOUNTS_PREFIX = "private_mount"

	// A tmpfs is memory, so it's bound to be small next to the job's own. This is only
	// to not quietly make a huge dump out of one that isn't, going by the size of the files
	// in it as that's what ends up in the dump. A sparse file takes up next to nothing of the tmpfs.
	PRIVATE_MOUNTS_MAX_SIZE     = 1 << 30
	PRIVATE_MOUNTS_MAX_SIZE_ENV = "CEDANA_SLURM_PRIVATE_MOUNTS_MAX_SIZE"
)

type privateMount struct {
	Mountpoint string `json:"mountpoint"`
	FSType     string `json:"fstype"`
	Archive    string `json:"archive"`
}

// pathInJob is path as seen from the mount namespace of pid
func pathInJob(pid uint32, path string) string {
	return filepath.Join(fmt.Sprintf("/proc/%d/root", pid), path)
}

func maxPrivateMountsSize() uint64 {
	if env := os.Getenv(PRIVATE_MOUNTS_MAX_SIZE_ENV); env != "" {
		max, err := strconv.ParseUint(env, 10, 64)
		if err == nil {
			return max
		}
		log.Warn().Err(err).Msgf("invalid %s, using default", PRIVATE_MOUNTS_MAX_SIZE_ENV)
	}
	return PRIVATE_MOUNTS_MAX_SIZE
}

func savePrivateMounts(fs afero.Fs, mounts []privateMount) error {
	file, err := fs.Create(PRIVATE_MOUNTS_FILE)
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(mounts)
}

// loadPrivateMounts returns nothing if the dump has no private mounts
func loadPrivateMounts(fs afero.Fs) ([]privateMount, error) {
	file, err := fs.Open(PRIVATE_MOUNTS_FILE)
	if err != nil {
		// Anything else (e.g. a failing streamer) is not the same as there being none
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var mounts []privateMount
	if err := json.Unmarshal(contents, &mounts); err != nil {
		return nil, err
	}

	for _, m := range mounts {
		// Only ever a name directly in the dump
		if m.Archive == "" || filepath.Base(m.Archive) != m.Archive || !filepath.IsAbs(m.Mountpoint) {
			return nil, fmt.Errorf("invalid private mount %+v", m)
		}
	}

	return mounts, nil
}
