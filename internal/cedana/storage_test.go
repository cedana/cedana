package cedana

import (
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCheckLocalCheckpoint(t *testing.T) {
	root := t.TempDir()
	write := func(name string) string {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	tarball := write("ckpt.tar.lz4")
	plain := write("notes.txt")
	write("criu/inventory.img")
	write("streamed/img-0")
	write("other/file")

	cases := []struct {
		name string
		path string
		code codes.Code
	}{
		{"tarball", tarball, codes.OK},
		{"criu dump dir", filepath.Join(root, "criu"), codes.OK},
		{"streamed dump dir", filepath.Join(root, "streamed"), codes.OK},
		{"plain file", plain, codes.InvalidArgument},
		{"unrelated dir", filepath.Join(root, "other"), codes.InvalidArgument},
		{"root", "/", codes.InvalidArgument},
		{"relative", "ckpt.tar", codes.InvalidArgument},
		{"missing", filepath.Join(root, "gone.tar"), codes.NotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkLocalCheckpoint(tc.path)
			if got := status.Code(err); got != tc.code {
				t.Fatalf("checkLocalCheckpoint(%q) = %v, want code %v", tc.path, err, tc.code)
			}
		})
	}
}
