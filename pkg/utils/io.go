package utils

import "io"

// CopyNotify asynchronously does io.Copy, notifying when done.
func CopyNotify(dst io.Writer, src io.Reader) chan any {
	done := make(chan any)
	go func() {
		io.Copy(dst, src)
		close(done)
	}()
	return done
}
