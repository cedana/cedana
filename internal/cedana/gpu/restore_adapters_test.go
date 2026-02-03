package gpu

import (
	"context"
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInheritFilesForRestore_Integration(t *testing.T) {
	testCases := []struct {
		name            string
		checkpointFiles []struct {
			filename string
			size     uint64
			baseAddr uint64
			segName  string
			pid      int
		}
		openFiles     []*daemon.File
		expectError   bool
		errorContains string
	}{
		{
			name: "creates all hostmem files with different worker/context indexes",
			checkpointFiles: []struct {
				filename string
				size     uint64
				baseAddr uint64
				segName  string
				pid      int
			}{
				{"gpu-hostmem-0-0", 1048576, 0x1000000000000, "hostmem-100", 100},
				{"gpu-hostmem-0-1", 2097152, 0x2000000000000, "hostmem-200", 200},
				{"gpu-hostmem-1-0", 4194304, 0x3000000000000, "hostmem-293", 293},
				{"gpu-hostmem-1-1", 8388608, 0x4000000000000, "hostmem-400", 400},
				{"gpu-hostmem-2-3", 16777216, 0x5000000000000, "hostmem-500", 500},
			},
			openFiles: []*daemon.File{
				{Path: "/dev/shm/cedana-gpu.container.misc/hostmem-100", Fd: 24, MountID: 4999, Inode: 216},
				{Path: "/dev/shm/cedana-gpu.container.misc/hostmem-200", Fd: 25, MountID: 4999, Inode: 217},
				{Path: "/dev/shm/cedana-gpu.container.misc/hostmem-293", Fd: 27, MountID: 4999, Inode: 219},
				{Path: "/dev/shm/cedana-gpu.container.misc/hostmem-400", Fd: 28, MountID: 4999, Inode: 220},
				{Path: "/dev/shm/cedana-gpu.container.misc/hostmem-500", Fd: 29, MountID: 4999, Inode: 221},
			},
			expectError: false,
		},
		{
			name: "handles single hostmem file",
			checkpointFiles: []struct {
				filename string
				size     uint64
				baseAddr uint64
				segName  string
				pid      int
			}{
				{"gpu-hostmem-1-0", 4194304, 0x3000000000000, "hostmem-293", 293},
			},
			openFiles: []*daemon.File{
				{Path: "/dev/shm/cedana-gpu.container.misc/hostmem-293", Fd: 27, MountID: 4999, Inode: 219},
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			gpuID := "test-gpu-id"
			ctx = context.WithValue(ctx, keys.GPU_ID_CONTEXT_KEY, gpuID)

			dumpFs := afero.NewMemMapFs()
			hostFs := afero.NewMemMapFs()

			for _, cf := range tc.checkpointFiles {
				metadata := make([]byte, 144)
				binary.LittleEndian.PutUint64(metadata[0:8], cf.size)
				binary.LittleEndian.PutUint64(metadata[8:16], cf.baseAddr)
				copy(metadata[16:], []byte("/"+cf.segName))

				err := afero.WriteFile(dumpFs, cf.filename, metadata, 0o644)
				require.NoError(t, err)
			}

			err := hostFs.MkdirAll("/dev/shm/cedana-gpu.test-gpu-id.misc", 0o755)
			require.NoError(t, err)

			err = os.MkdirAll("/dev/shm", 0o755)
			require.NoError(t, err)
			controllerShmFile, err := os.Create("/dev/shm/cedana-gpu.test-gpu-id")
			require.NoError(t, err)
			controllerShmFile.Close()
			defer os.Remove("/dev/shm/cedana-gpu.test-gpu-id")

			state := &daemon.ProcessState{
				GPUID:      "old-gpu-id",
				GPUEnabled: true,
				OpenFiles:  tc.openFiles,
			}

			req := &daemon.RestoreReq{
				Criu: &criu.CriuOpts{},
				UID:  1000,
				GID:  1000,
			}

			resp := &daemon.RestoreResp{
				State: state,
			}

			opts := types.Opts{
				DumpFs:       dumpFs,
				HostFs:       hostFs,
				ExtraFiles:   []*os.File{},
				InheritFdMap: make(map[string]int32),
			}

			nextCalled := false
			mockNext := func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
				nextCalled = true
				return nil, nil
			}

			handler := InheritFilesForRestore(mockNext)
			_, err = handler(ctx, opts, resp, req)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
				return
			}

			assert.NoError(t, err)
			assert.True(t, nextCalled, "next handler should be called")

			for _, cf := range tc.checkpointFiles {
				expectedPath := "/dev/shm/cedana-gpu.test-gpu-id.misc/hostmem-" + strconv.Itoa(cf.pid)
				fileInfo, statErr := hostFs.Stat(expectedPath)
				require.NoError(t, statErr, "hostmem file should be created at %s", expectedPath)
				assert.Equal(t, int64(cf.size), fileInfo.Size(),
					"hostmem-%d file should be truncated to correct size", cf.pid)
			}

			hostmemFilesCreated := 0
			afero.Walk(hostFs, "/dev/shm/cedana-gpu.test-gpu-id.misc", func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() && strings.Contains(path, "hostmem-") {
					hostmemFilesCreated++
				}
				return nil
			})

			assert.Equal(t, len(tc.checkpointFiles), hostmemFilesCreated,
				"should create exactly %d hostmem files", len(tc.checkpointFiles))
		})
	}
}
