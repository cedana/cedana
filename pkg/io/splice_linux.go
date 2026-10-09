package io

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

const (
	// Max bytes moved per splice(2) call. Bounded by the pipe capacity anyway.
	SPLICE_CHUNK = 4 << 20
	// Bytes to prefetch ahead of the splice cursor with FADV_WILLNEED.
	READAHEAD_WINDOW = 64 << 20
)

// SpliceFrom moves the entire contents of src (a regular file) into dst (a pipe)
// using splice(2), so the data never passes through userspace. It tells the
// kernel that src is read sequentially, prefetches a window ahead of the cursor,
// and drops already-consumed pages from the page cache behind it.
func SpliceFrom(src *os.File, dst *os.File) (n int64, err error) {
	srcFd, dstFd := int(src.Fd()), int(dst.Fd())

	unix.Fadvise(srcFd, 0, 0, unix.FADV_SEQUENTIAL)
	unix.Fadvise(srcFd, 0, READAHEAD_WINDOW, unix.FADV_WILLNEED)
	defer unix.Fadvise(srcFd, 0, 0, unix.FADV_DONTNEED)

	var nextPrefetch int64 = READAHEAD_WINDOW
	var lastDrop int64
	for {
		w, err := unix.Splice(srcFd, nil, dstFd, nil, SPLICE_CHUNK, unix.SPLICE_F_MORE)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return n, err
		}
		if w == 0 {
			return n, nil // EOF
		}
		n += w
		if n >= nextPrefetch {
			unix.Fadvise(srcFd, nextPrefetch, READAHEAD_WINDOW, unix.FADV_WILLNEED)
			nextPrefetch += READAHEAD_WINDOW
		}
		if n-lastDrop >= 2*READAHEAD_WINDOW {
			unix.Fadvise(srcFd, lastDrop, READAHEAD_WINDOW, unix.FADV_DONTNEED)
			lastDrop += READAHEAD_WINDOW
		}
	}
}
