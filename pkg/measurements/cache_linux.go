//go:build linux

package measurements

import (
	"os"

	"golang.org/x/sys/unix"
)

func evictFileCache(file *os.File) error {
	return unix.Fadvise(int(file.Fd()), 0, 0, unix.FADV_DONTNEED)
}
