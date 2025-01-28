package streamer

import (
	"fmt"
	"os"
	"syscall"
)

// Implementation of the afero.File interface that uses streaming as the backend
type File struct {
	name string
	mode Mode
	fd   int
}

func (f *File) Name() string {
	return f.name
}

func (f *File) Read(p []byte) (n int, err error) {
	if f.mode != READ_ONLY {
		return 0, fmt.Errorf("file is not open for reading")
	}
	return syscall.Read(f.fd, p)
}

func (f *File) Write(p []byte) (n int, err error) {
	if f.mode != WRITE_ONLY {
		return 0, fmt.Errorf("file is not open for writing")
	}
	return syscall.Write(f.fd, p)
}

func (f *File) Truncate(size int64) error {
	if f.mode != WRITE_ONLY {
		return fmt.Errorf("file is not open for writing")
	}
	return syscall.Ftruncate(f.fd, size)
}

func (f *File) WriteString(s string) (ret int, err error) {
	if f.mode != WRITE_ONLY {
		return 0, fmt.Errorf("file is not open for writing")
	}
	return syscall.Write(f.fd, []byte(s))
}

func (f *File) Close() (err error) {
	return syscall.Close(f.fd)
}

func (f *File) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, fmt.Errorf("not implemented for streaming")
}

func (f *File) Seek(offset int64, whence int) (int64, error) {
	return 0, fmt.Errorf("not implemented for streaming")
}

func (f *File) WriteAt(p []byte, off int64) (n int, err error) {
	return 0, fmt.Errorf("not implemented for streaming")
}

func (f *File) Readdir(count int) ([]os.FileInfo, error) {
	return nil, fmt.Errorf("not implemented for streaming")
}

func (f *File) Readdirnames(n int) ([]string, error) {
	return nil, fmt.Errorf("not implemented for streaming")
}

func (f *File) Stat() (os.FileInfo, error) {
	return nil, fmt.Errorf("not implemented for streaming")
}

func (f *File) Sync() error {
	return fmt.Errorf("not implemented for streaming")
}
