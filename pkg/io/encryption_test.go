package io

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func encrypt(t *testing.T, key []byte, name string, plain []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := NewEncryptionWriter(&out, key, name)
	if err != nil {
		t.Fatal(err)
	}
	// Uneven writes, so segment boundaries do not line up with them
	for p := plain; len(p) > 0; {
		n := min(len(p), 333_333)
		if _, err := w.Write(p[:n]); err != nil {
			t.Fatal(err)
		}
		p = p[n:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func decrypt(key []byte, name string, sealed []byte) ([]byte, error) {
	r, err := NewEncryptionReader(bytes.NewReader(sealed), key, name)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEncryptionRoundTrip(t *testing.T) {
	key := randomBytes(t, KEY_SIZE)
	for _, n := range []int{0, 1, SEGMENT_SIZE - 1, SEGMENT_SIZE, SEGMENT_SIZE + 1, 3*SEGMENT_SIZE + 5} {
		plain := randomBytes(t, n)
		got, err := decrypt(key, "img-0", encrypt(t, key, "img-0", plain))
		if err != nil {
			t.Fatalf("size %d: %v", n, err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("size %d: round trip differs", n)
		}
	}
}

func TestEncryptionRejects(t *testing.T) {
	key := randomBytes(t, KEY_SIZE)
	plain := randomBytes(t, 2*SEGMENT_SIZE+100)
	sealed := encrypt(t, key, "img-0", plain)
	seg := SEGMENT_SIZE + tagSize

	flipped := bytes.Clone(sealed)
	flipped[headerSize+seg+10] ^= 1

	swapped := bytes.Clone(sealed)
	copy(swapped[headerSize:], sealed[headerSize+seg:headerSize+2*seg])
	copy(swapped[headerSize+seg:], sealed[headerSize:headerSize+seg])

	cases := map[string]struct {
		key, sealed []byte
		name        string
	}{
		"wrong key":          {randomBytes(t, KEY_SIZE), sealed, "img-0"},
		"other file's name":  {key, sealed, "img-1"},
		"flipped bit":        {key, flipped, "img-0"},
		"swapped segments":   {key, swapped, "img-0"},
		"truncated mid-file": {key, sealed[:len(sealed)-50], "img-0"},
		"last segment gone":  {key, sealed[:headerSize+2*seg], "img-0"},
		"header only":        {key, sealed[:headerSize], "img-0"},
	}
	for name, c := range cases {
		if _, err := decrypt(c.key, c.name, c.sealed); !errors.Is(err, ErrDecrypt) {
			t.Errorf("%s: want ErrDecrypt, got %v", name, err)
		}
	}
}

func TestIsEncrypted(t *testing.T) {
	if enc, base := IsEncrypted("/x/img-0.lz4.enc"); !enc || base != "/x/img-0.lz4" {
		t.Fatalf("got %v %q", enc, base)
	}
	if enc, _ := IsEncrypted("/x/img-0.lz4"); enc {
		t.Fatal("plain file reported encrypted")
	}
}
