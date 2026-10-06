package io

import (
	"bytes"
	"hash/crc32"
	"strings"
	"testing"
)

func TestChecksumWriterHashesWhatItWrites(t *testing.T) {
	var buf bytes.Buffer
	w := NewChecksumWriter(&buf)
	for _, part := range []string{"check", "point"} {
		if _, err := w.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	want := FormatChecksum(crc32.Checksum([]byte("checkpoint"), crc32.MakeTable(crc32.Castagnoli)))
	if got := w.Sum(); got != want {
		t.Fatalf("Sum = %s, want %s", got, want)
	}
	if buf.String() != "checkpoint" {
		t.Fatalf("underlying writer got %q", buf.String())
	}
	if !strings.HasPrefix(want, "crc32c:") || len(want) != len("crc32c:")+8 {
		t.Fatalf("checksum %q is not crc32c: and eight hex digits", want)
	}
}

func TestChecksumOfMatchesWriter(t *testing.T) {
	data := bytes.Repeat([]byte("abc"), 10000)
	fromReader, err := ChecksumOf(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	w := NewChecksumWriter(&bytes.Buffer{})
	w.Write(data)
	if fromReader != w.Sum() {
		t.Fatalf("ChecksumOf = %s, writer = %s", fromReader, w.Sum())
	}
	// A known value: CRC32C("123456789") is 0xe3069283
	known, _ := ChecksumOf(strings.NewReader("123456789"))
	if known != "crc32c:e3069283" {
		t.Fatalf("CRC32C of the check string = %s", known)
	}
}

func TestManifestChecksum(t *testing.T) {
	entries := []ManifestEntry{{"img-0", "crc32c:00000001"}, {"img-1", "crc32c:00000002"}}
	manifest := "img-0 crc32c:00000001\nimg-1 crc32c:00000002\n"
	want, _ := ChecksumOf(strings.NewReader(manifest))
	if got := ManifestChecksum(entries); got != want {
		t.Fatalf("ManifestChecksum = %s, want %s", got, want)
	}
	if got := ManifestChecksum(nil); got != "" {
		t.Fatalf("an empty manifest must have no checksum, got %s", got)
	}
	// The order is the caller's: a different order is a different checksum
	if ManifestChecksum([]ManifestEntry{entries[1], entries[0]}) == want {
		t.Fatal("the manifest must depend on the order of its entries")
	}
}
