package overlay

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"

	runc_proto "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"google.golang.org/protobuf/proto"
)

func delimited(t *testing.T, msgs ...*runc_proto.RWFile) *bufio.Reader {
	t.Helper()
	var b bytes.Buffer
	for _, m := range msgs {
		data, err := proto.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		binary.Write(&b, binary.LittleEndian, uint32(len(data)))
		b.Write(data)
	}
	return bufio.NewReader(&b)
}

// Content is written from every chunk until one without Content entries, which ends the
// file, as proto-decoding each chunk did
func TestWriteFileContent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []*runc_proto.RWFile
		want   string
		rest   int // messages left for the next file
	}{
		{"chunks then terminator", []*runc_proto.RWFile{
			{Content: [][]byte{[]byte("ab"), []byte("c")}},
			{Content: [][]byte{[]byte("de")}},
			{},
			{Path: "next"},
		}, "abcde", 1},
		{"empty entries do not end the file", []*runc_proto.RWFile{
			{Content: [][]byte{{}}},
			{Content: [][]byte{[]byte("x")}},
			{},
		}, "x", 0},
		{"other fields are skipped", []*runc_proto.RWFile{
			{Path: "ignored", Mode: 0o644, Content: [][]byte{[]byte("y")}},
			{},
		}, "y", 0},
		{"EOF ends the file", []*runc_proto.RWFile{
			{Content: [][]byte{[]byte("z")}},
		}, "z", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := delimited(t, tc.chunks...)
			var out bytes.Buffer
			if err := writeFileContent(r, &out); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Errorf("content = %q, want %q", out.String(), tc.want)
			}
			left := 0
			var buf []byte
			for {
				if _, err := readDelimitedMessageInto(r, &buf); err != nil {
					break
				}
				left++
			}
			if left != tc.rest {
				t.Errorf("%d messages left, want %d", left, tc.rest)
			}
		})
	}
}

func TestWriteChunkContentCorrupt(t *testing.T) {
	if _, err := writeChunkContent([]byte{0x5a, 0xff}, &bytes.Buffer{}); err == nil {
		t.Error("expected an error for a truncated chunk")
	}
}
