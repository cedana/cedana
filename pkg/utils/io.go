package utils

import "io"

// CopyNotify asynchronously does io.Copy, notifying when done.
func CopyNotify(dst io.Writer, src io.Reader) chan error {
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(dst, src)
		done <- err
		close(done)
	}()
	return done
}
