package streamer

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// Implementation of the afero.File interface that uses streaming as the backend
type File struct {
	name      string
	mode      Mode
	pipe      int
	fs        *Fs      // reference to parent filesystem for directory operations
	dirNames  []string // cached directory listing
	dirOffset int      // current position in directory listing
}

func (f *File) Name() string {
	return f.name
}

func (f *File) Read(p []byte) (n int, err error) {
	if f.mode != READ_ONLY {
		return 0, fmt.Errorf("file is not open for reading")
	}
	n, err = syscall.Read(f.pipe, p)
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
}

func (f *File) Write(p []byte) (n int, err error) {
	if f.mode != WRITE_ONLY {
		return 0, fmt.Errorf("file is not open for writing")
	}
	total := 0
	for total < len(p) {
		n, err = syscall.Write(f.pipe, p[total:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (f *File) Truncate(size int64) error {
	if f.mode != WRITE_ONLY {
		return fmt.Errorf("file is not open for writing")
	}
	return syscall.Ftruncate(f.pipe, size)
}

func (f *File) WriteString(s string) (ret int, err error) {
	if f.mode != WRITE_ONLY {
		return 0, fmt.Errorf("file is not open for writing")
	}
	return f.Write([]byte(s))
}

func (f *File) Close() (err error) {
	if f.pipe < 0 {
		return nil // already closed
	}
	err = syscall.Close(f.pipe)
	if err == nil {
		f.pipe = -1
	}
	return err
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
	// Only works for the root directory in READ_ONLY mode
	if f.mode != READ_ONLY {
		return nil, fmt.Errorf("readdirnames: not open for reading")
	}
	
	if f.name != "." && f.name != "/" && f.name != "" {
		return nil, fmt.Errorf("readdirnames: only root directory supported")
	}
	
	// Lazy load directory listing using Glob
	if f.dirNames == nil && f.fs != nil {
		names, err := f.fs.Glob("*")
		if err != nil {
			return nil, fmt.Errorf("readdirnames: failed to list files: %w", err)
		}
		f.dirNames = names
		f.dirOffset = 0
	}
	
	// Return all remaining names if n <= 0
	if n <= 0 {
		names := f.dirNames[f.dirOffset:]
		f.dirOffset = len(f.dirNames)
		if len(names) == 0 {
			return nil, io.EOF
		}
		return names, nil
	}
	
	// Return up to n names
	if f.dirOffset >= len(f.dirNames) {
		return nil, io.EOF
	}
	
	end := f.dirOffset + n
	if end > len(f.dirNames) {
		end = len(f.dirNames)
	}
	
	names := f.dirNames[f.dirOffset:end]
	f.dirOffset = end
	return names, nil
}

func (f *File) Stat() (os.FileInfo, error) {
	return nil, fmt.Errorf("not implemented for streaming")
}

func (f *File) Sync() error {
	return fmt.Errorf("not implemented for streaming")
}

