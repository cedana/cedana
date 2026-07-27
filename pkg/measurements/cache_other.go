//go:build !linux

package measurements

import "os"

func evictFileCache(file *os.File) error {
	return errCacheEvictionUnsupported
}
