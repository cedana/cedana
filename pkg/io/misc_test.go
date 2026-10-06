package io

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

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
