package streamer

import (
	"fmt"
	"os"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// Implementation of the afero.File interface that uses streaming as the backend
type File struct {
	name string
	rFd  int // read file descriptor
	wFd  int // write file descriptor
	afero.File
}

func (f *File) Name() string {
	return f.name
}

func (f *File) Read(p []byte) (n int, err error) {
  log.Warn().Msg("Read called")
	n, err = syscall.Read(f.rFd, p)
  log.Warn().Msgf("Read called: %d", n)
  return
}

func (f *File) Write(p []byte) (n int, err error) {
	return syscall.Write(f.wFd, p)
}

func (f *File) Truncate(size int64) error {
	return syscall.Ftruncate(f.wFd, size)
}

func (f *File) WriteString(s string) (ret int, err error) {
	return syscall.Write(f.wFd, []byte(s))
}

func (f *File) Close() (err error) {
	err = syscall.Close(f.rFd)
	if err != nil {
		return
	}
	err = syscall.Close(f.wFd)
	return
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
