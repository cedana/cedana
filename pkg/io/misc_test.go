package io

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUntarPaths(t *testing.T) {
	for _, tc := range []struct {
		name    string
		wantErr bool
	}{
		{"checkpoint.json", false},
		{"checkpoint..json", false},
		{"nested/../checkpoint.json", false},
		{"../outside", true},
		{"nested/../../outside", true},
		{"/absolute", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "dest")
			if err := os.Mkdir(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			name := tc.name
			if filepath.IsAbs(name) {
				name = filepath.Join(root, "absolute")
			}

			var archive bytes.Buffer
			writer := tar.NewWriter(&archive)
			data := []byte("checkpoint contents")
			if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			err := Untar(&archive, dest, "tar")
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "invalid file path") {
					t.Fatalf("got %v, want an invalid file path error", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(dest, name))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, data) {
				t.Errorf("got %q, want %q", got, data)
			}
		})
	}
}

func TestListTarAndOpenTarEntry(t *testing.T) {
	for _, compression := range []string{"tar", "gzip", "lz4", "zlib"} {
		t.Run(compression, func(t *testing.T) {
			src := t.TempDir()
			small := []byte("hello")
			large := bytes.Repeat([]byte("x"), 4096)
			if err := os.WriteFile(filepath.Join(src, "a.img"), small, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(src, "b.img"), large, 0o644); err != nil {
				t.Fatal(err)
			}

			var tarball bytes.Buffer
			if err := Tar(src, &tarball, compression, false); err != nil {
				t.Fatal(err)
			}

			entries, err := ListTar(bytes.NewReader(tarball.Bytes()), compression)
			if err != nil {
				t.Fatal(err)
			}
			sizes := map[string]int64{}
			for _, entry := range entries {
				if !entry.IsDir {
					sizes[entry.Name] = entry.Size
				}
			}
			if len(sizes) != 2 || sizes["a.img"] != int64(len(small)) || sizes["b.img"] != int64(len(large)) {
				t.Fatalf("unexpected entries: %+v", entries)
			}

			reader, err := OpenTarEntry(bytes.NewReader(tarball.Bytes()), compression, "b.img")
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, large) {
				t.Fatalf("entry contents mismatch: got %d bytes", len(data))
			}

			if _, err := OpenTarEntry(bytes.NewReader(tarball.Bytes()), compression, "missing.img"); err == nil {
				t.Fatal("expected error for missing entry")
			}
		})
	}
}

func TestTarCompressionFromPath(t *testing.T) {
	cases := []struct {
		path        string
		compression string
		ok          bool
	}{
		{"csx://1234.tar.lz4", "lz4", true},
		{"s3://bucket/dir/ckpt.tar.gz", "gzip", true},
		{"/tmp/ckpt.tar.zlib", "zlib", true},
		{"/tmp/ckpt.tar", "tar", true},
		{"/tmp/ckpt", "", false},
		{"s3://bucket/dir/pages-1.img", "", false},
		{"/tmp/ckpt.tar.bogus", "", false},
	}
	for _, c := range cases {
		compression, ok := TarCompressionFromPath(c.path)
		if compression != c.compression || ok != c.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.path, compression, ok, c.compression, c.ok)
		}
	}
}
