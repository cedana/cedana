package io

import (
	"golang.org/x/sys/unix"
)

const CHUNK_SIZE = 4 * MEBIBYTE

// Splice copies data from srcFd to dstFd using the splice system call, until EOF is reached.
// which moves data between two file descriptors without copying it b/w kernel and user space.
// One of the file descriptors must be a pipe. Check out splice(2) man page for more information.
func Splice(srcFd, dstFd uintptr) (int64, error) {
	var total int64
	for {
		n, err := unix.Splice(int(srcFd), nil, int(dstFd), nil, CHUNK_SIZE, unix.SPLICE_F_MOVE)
		if err != nil {
			return total, err
		}
		if n == 0 {
			break
		}
		total += int64(n)
	}
	return total, nil
}

func IsPipe(fd uintptr) (bool, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(fd), &stat); err != nil {
		return false, err
	}
	return stat.Mode&unix.S_IFIFO != 0, nil
}
