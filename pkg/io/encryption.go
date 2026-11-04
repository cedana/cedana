package io

// Streaming authenticated encryption for checkpoint files. A file is a header, then segments of at
// most SEGMENT_SIZE plaintext bytes, each sealed with AES-256-GCM under a per-file key derived from
// the checkpoint key and the header's salt. A segment's nonce holds its index and whether it is
// the last one, and the file name is bound in, so a reordered, swapped or truncated file fails.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	ENCRYPTED_EXT = ".enc"
	KEY_SIZE      = 32
	SEGMENT_SIZE  = 1 << 20

	encMagic   = "CDNENC1\x00"
	saltSize   = 32
	prefixSize = 7 // nonce: prefix | segment index (4) | last flag (1)
	headerSize = len(encMagic) + saltSize + prefixSize
	tagSize    = 16
)

var ErrDecrypt = errors.New("checkpoint decryption failed: wrong key, or the file was modified")

// LoadEncryptionKey reads a raw 32-byte key from path.
func LoadEncryptionKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read checkpoint encryption key: %w", err)
	}
	if len(key) != KEY_SIZE {
		return nil, fmt.Errorf("checkpoint encryption key %s must be %d raw bytes, got %d", path, KEY_SIZE, len(key))
	}
	return key, nil
}

// IsEncrypted reports whether path names an encrypted file, and returns the name without the suffix.
func IsEncrypted(path string) (bool, string) {
	return strings.HasSuffix(path, ENCRYPTED_EXT), strings.TrimSuffix(path, ENCRYPTED_EXT)
}

func fileAEAD(key, salt []byte, name string) (cipher.AEAD, error) {
	if len(key) != KEY_SIZE {
		return nil, fmt.Errorf("checkpoint encryption key must be %d bytes", KEY_SIZE)
	}
	fileKey, err := hkdf.Key(sha256.New, key, salt, "cedana checkpoint v1 "+name, KEY_SIZE)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(fileKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func segmentNonce(prefix []byte, index uint32, last bool) []byte {
	nonce := make([]byte, 12)
	copy(nonce, prefix)
	binary.BigEndian.PutUint32(nonce[prefixSize:], index)
	if last {
		nonce[11] = 1
	}
	return nonce
}

type encryptionWriter struct {
	w      io.Writer
	aead   cipher.AEAD
	prefix []byte
	name   []byte
	buf    []byte
	index  uint32
	closed bool
}

// NewEncryptionWriter seals everything written to it into w; Close writes the final segment and
// must be called for the file to decrypt.
func NewEncryptionWriter(w io.Writer, key []byte, name string) (io.WriteCloser, error) {
	header := make([]byte, headerSize)
	copy(header, encMagic)
	if _, err := rand.Read(header[len(encMagic):]); err != nil {
		return nil, err
	}
	salt := header[len(encMagic) : len(encMagic)+saltSize]
	aead, err := fileAEAD(key, salt, name)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(header); err != nil {
		return nil, err
	}
	return &encryptionWriter{
		w:      w,
		aead:   aead,
		prefix: header[len(encMagic)+saltSize:],
		name:   []byte(name),
		buf:    make([]byte, 0, SEGMENT_SIZE),
	}, nil
}

func (e *encryptionWriter) seal(last bool) error {
	if e.index == ^uint32(0) {
		return fmt.Errorf("checkpoint file too large to encrypt")
	}
	out := e.aead.Seal(nil, segmentNonce(e.prefix, e.index, last), e.buf, e.name)
	e.index++
	e.buf = e.buf[:0]
	_, err := e.w.Write(out)
	return err
}

func (e *encryptionWriter) Write(p []byte) (int, error) {
	n := 0
	for len(p) > 0 {
		// A full segment is sealed only once more data arrives, so the last one is always marked last
		if len(e.buf) == SEGMENT_SIZE {
			if err := e.seal(false); err != nil {
				return n, err
			}
		}
		c := copy(e.buf[len(e.buf):SEGMENT_SIZE], p)
		e.buf = e.buf[:len(e.buf)+c]
		p = p[c:]
		n += c
	}
	return n, nil
}

func (e *encryptionWriter) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	return e.seal(true)
}

type encryptionReader struct {
	r      io.Reader
	aead   cipher.AEAD
	prefix []byte
	name   []byte
	in     []byte
	out    []byte
	carry  int // bytes of the next segment already read into in[0]
	index  uint32
	done   bool
}

// NewEncryptionReader opens what NewEncryptionWriter wrote under the same key and name.
func NewEncryptionReader(r io.Reader, key []byte, name string) (io.ReadCloser, error) {
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("%w: short header", ErrDecrypt)
	}
	if string(header[:len(encMagic)]) != encMagic {
		return nil, fmt.Errorf("%w: not an encrypted checkpoint file", ErrDecrypt)
	}
	aead, err := fileAEAD(key, header[len(encMagic):len(encMagic)+saltSize], name)
	if err != nil {
		return nil, err
	}
	return &encryptionReader{
		r:      r,
		aead:   aead,
		prefix: header[len(encMagic)+saltSize:],
		name:   []byte(name),
		in:     make([]byte, SEGMENT_SIZE+tagSize+1),
	}, nil
}

func (d *encryptionReader) Read(p []byte) (int, error) {
	for len(d.out) == 0 {
		if d.done {
			return 0, io.EOF
		}
		if err := d.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, d.out)
	d.out = d.out[n:]
	return n, nil
}

// next opens one segment. Reading one byte past a full segment tells whether more follow.
func (d *encryptionReader) next() error {
	n, err := io.ReadFull(d.r, d.in[d.carry:])
	n += d.carry
	switch {
	case err == nil: // a full segment with more after it: carry the extra byte to the next one
		seg := d.in[:SEGMENT_SIZE+tagSize]
		plain, oerr := d.aead.Open(nil, segmentNonce(d.prefix, d.index, false), seg, d.name)
		if oerr != nil {
			return ErrDecrypt
		}
		d.in[0] = d.in[SEGMENT_SIZE+tagSize]
		d.carry = 1
		d.out = plain
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		plain, oerr := d.aead.Open(nil, segmentNonce(d.prefix, d.index, true), d.in[:n], d.name)
		if oerr != nil {
			return ErrDecrypt
		}
		d.out = plain
		d.done = true
	default:
		return err
	}
	d.index++
	return nil
}

func (d *encryptionReader) Close() error {
	return nil
}
