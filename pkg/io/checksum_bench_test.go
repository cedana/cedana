package io

import (
	"bytes"
	"crypto/sha256"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// Data that compresses about like a checkpoint: half random, half repeated
func benchData(size int) []byte {
	data := make([]byte, size)
	r := rand.New(rand.NewSource(1))
	for i := 0; i < size; i += 2 * 4096 {
		end := min(i+4096, size)
		r.Read(data[i:end])
	}
	return data
}

const benchSize = 64 << 20

// The hash alone: what one core computes per second
func BenchmarkCRC32C(b *testing.B) {
	data := benchData(benchSize)
	b.SetBytes(benchSize)
	b.ResetTimer()
	for b.Loop() {
		if _, err := ChecksumOf(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

// The hash the csx design used, for comparison
func BenchmarkSHA256(b *testing.B) {
	data := benchData(benchSize)
	b.SetBytes(benchSize)
	b.ResetTimer()
	for b.Loop() {
		h := sha256.New()
		if _, err := io.Copy(h, bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
		_ = h.Sum(nil)
	}
}

// A write through the ChecksumWriter against a plain write, both to a sink
func BenchmarkWriteWithoutChecksum(b *testing.B) {
	data := benchData(benchSize)
	b.SetBytes(benchSize)
	b.ResetTimer()
	for b.Loop() {
		if _, err := io.Copy(io.Discard, bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteWithChecksum(b *testing.B) {
	data := benchData(benchSize)
	b.SetBytes(benchSize)
	b.ResetTimer()
	for b.Loop() {
		w := NewChecksumWriter(io.Discard)
		if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
		_ = w.Sum()
	}
}

// The streamer's write path: lz4 compression of a shard, with and without the
// hash in the chain, as the daemon does for a streamed checkpoint
func benchWriteTo(b *testing.B, compression string, hash bool) {
	path := filepath.Join(b.TempDir(), "shard")
	if err := os.WriteFile(path, benchData(benchSize), 0o644); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(benchSize)
	b.ResetTimer()
	for b.Loop() {
		src, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		var dst io.Writer = io.Discard
		var hasher *ChecksumWriter
		if hash {
			hasher = NewChecksumWriter(io.Discard)
			dst = hasher
		}
		if _, err := WriteTo(src, dst, compression); err != nil {
			b.Fatal(err)
		}
		src.Close()
		if hasher != nil {
			_ = hasher.Sum()
		}
	}
}

func BenchmarkWriteToLZ4(b *testing.B)              { benchWriteTo(b, "lz4", false) }
func BenchmarkWriteToLZ4WithChecksum(b *testing.B)  { benchWriteTo(b, "lz4", true) }
func BenchmarkWriteToGzip(b *testing.B)             { benchWriteTo(b, "gzip", false) }
func BenchmarkWriteToGzipWithChecksum(b *testing.B) { benchWriteTo(b, "gzip", true) }

// The compress of a bundled checkpoint, tar of a directory, with and without the hash
func benchTar(b *testing.B, compression string, hash bool) {
	dir := b.TempDir()
	for i := range 8 {
		if err := os.WriteFile(filepath.Join(dir, "pages-"+string(rune('a'+i))+".img"), benchData(benchSize/8), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(benchSize)
	b.ResetTimer()
	for b.Loop() {
		var dst io.Writer = io.Discard
		var hasher *ChecksumWriter
		if hash {
			hasher = NewChecksumWriter(io.Discard)
			dst = hasher
		}
		if err := Tar(dir, dst, compression, false); err != nil {
			b.Fatal(err)
		}
		if hasher != nil {
			_ = hasher.Sum()
		}
	}
}

func BenchmarkTarLZ4(b *testing.B)             { benchTar(b, "lz4", false) }
func BenchmarkTarLZ4WithChecksum(b *testing.B) { benchTar(b, "lz4", true) }

// Benchmarks on a file of any size, for sizes a test cannot generate each time:
// CEDANA_BENCH_FILE names the file, which should compress about like a checkpoint.
// Skipped when it is not set. One iteration each; run with -count for more.
func benchFile(b *testing.B) string {
	path := os.Getenv("CEDANA_BENCH_FILE")
	if path == "" {
		b.Skip("CEDANA_BENCH_FILE not set")
	}
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(info.Size())
	return path
}

func BenchmarkFileCRC32C(b *testing.B) {
	path := benchFile(b)
	for b.Loop() {
		f, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := ChecksumOf(f); err != nil {
			b.Fatal(err)
		}
		f.Close()
	}
}

func benchFileWriteTo(b *testing.B, compression string, hash bool) {
	path := benchFile(b)
	for b.Loop() {
		src, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		var dst io.Writer = io.Discard
		var hasher *ChecksumWriter
		if hash {
			hasher = NewChecksumWriter(io.Discard)
			dst = hasher
		}
		if _, err := WriteTo(src, dst, compression); err != nil {
			b.Fatal(err)
		}
		src.Close()
		if hasher != nil {
			_ = hasher.Sum()
		}
	}
}

func BenchmarkFileWriteToLZ4(b *testing.B)             { benchFileWriteTo(b, "lz4", false) }
func BenchmarkFileWriteToLZ4WithChecksum(b *testing.B) { benchFileWriteTo(b, "lz4", true) }

// The compress of a bundled checkpoint whose directory holds the file. Every
// regular file of the directory goes into the tarball, so the bytes are theirs
func benchFileTar(b *testing.B, compression string, hash bool) {
	path := benchFile(b)
	var size int64
	err := filepath.WalkDir(filepath.Dir(path), func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(size)
	for b.Loop() {
		var dst io.Writer = io.Discard
		var hasher *ChecksumWriter
		if hash {
			hasher = NewChecksumWriter(io.Discard)
			dst = hasher
		}
		if err := Tar(filepath.Dir(path), dst, compression, false); err != nil {
			b.Fatal(err)
		}
		if hasher != nil {
			_ = hasher.Sum()
		}
	}
}

func BenchmarkFileTarLZ4(b *testing.B)             { benchFileTar(b, "lz4", false) }
func BenchmarkFileTarLZ4WithChecksum(b *testing.B) { benchFileTar(b, "lz4", true) }
