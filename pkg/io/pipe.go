package io

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

const (
	// Max bytes moved per splice(2) call. Bounded by the pipe capacity anyway.
	CHUNK_SIZE = 4 * MEBIBYTE
	// Bytes to prefetch ahead of the splice cursor with FADV_WILLNEED.
	READAHEAD_WINDOW = 64 * MEBIBYTE
)

// Splice copies data from srcFd to dstFd using the splice system call, until EOF is reached,
// which moves data between two file descriptors without copying it b/w kernel and user space.
// One of the file descriptors must be a pipe. Check out splice(2) man page for more information.
func Splice(srcFd, dstFd uintptr) (int64, error) {
	return splice(int(srcFd), int(dstFd), nil)
}

// SpliceFile moves the entire contents of src (a regular file) into dst (a pipe) using
// splice(2), so the data never passes through userspace. On top of Splice, it tells the
// kernel that src is read sequentially, prefetches a window ahead of the cursor, and drops
// already-consumed pages from the page cache behind it.
func SpliceFile(src *os.File, dst *os.File) (int64, error) {
	srcFd, dstFd := int(src.Fd()), int(dst.Fd())

	unix.Fadvise(srcFd, 0, 0, unix.FADV_SEQUENTIAL)
	unix.Fadvise(srcFd, 0, READAHEAD_WINDOW, unix.FADV_WILLNEED)
	defer unix.Fadvise(srcFd, 0, 0, unix.FADV_DONTNEED)

	var nextPrefetch int64 = READAHEAD_WINDOW
	var lastDrop int64
	return splice(srcFd, dstFd, func(total int64) {
		if total >= nextPrefetch {
			unix.Fadvise(srcFd, nextPrefetch, READAHEAD_WINDOW, unix.FADV_WILLNEED)
			nextPrefetch += READAHEAD_WINDOW
		}
		if total-lastDrop >= 2*READAHEAD_WINDOW {
			unix.Fadvise(srcFd, lastDrop, READAHEAD_WINDOW, unix.FADV_DONTNEED)
			lastDrop += READAHEAD_WINDOW
		}
	})
}

// splice runs the splice(2) loop until EOF, calling progress (if non-nil) with the running
// total after each chunk.
func splice(srcFd, dstFd int, progress func(total int64)) (total int64, err error) {
	for {
		n, err := unix.Splice(srcFd, nil, dstFd, nil, CHUNK_SIZE, unix.SPLICE_F_MOVE)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return total, err
		}
		if n == 0 {
			return total, nil // EOF
		}
		total += n
		if progress != nil {
			progress(total)
		}
	}
}

func IsPipe(fd uintptr) (bool, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(fd), &stat); err != nil {
		return false, err
	}
	return stat.Mode&unix.S_IFIFO != 0, nil
}
